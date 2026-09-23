#!/usr/bin/env bash
# Builds, pushes and deploys the checked-out commit. The deploy workflow runs
# this with the OIDC role; a person can run it from the repository root with
# their own credentials. Needs aws, terraform, docker buildx, jq, curl and git.
#
# Environment:
#   RELEASE             commit to deploy (default: HEAD); also the image tag
#   BASE_URL            public URL; empty keeps the one in state, or on the
#                       first deploy uses the CloudFront domain once it exists
#   GENERATION_ENABLED  true/false; pauses new generation when false
set -euo pipefail

cd "$(dirname "$0")/.."
export AWS_REGION=${AWS_REGION:-ap-northeast-1}
RELEASE=${RELEASE:-$(git rev-parse HEAD)}
BASE_URL=${BASE_URL:-}
GENERATION_ENABLED=${GENERATION_ENABLED:-true}
case $GENERATION_ENABLED in true | false) ;; *)
  echo "GENERATION_ENABLED must be true or false" >&2
  exit 1
  ;;
esac
if [[ ! $RELEASE =~ ^[0-9a-f]{40}$ ]]; then
  echo "RELEASE must be a full commit SHA: $RELEASE" >&2
  exit 1
fi
tf() { terraform -chdir=infra "$@"; }
output() { tf output -json | jq -r --arg k "$1" '.[$k].value // empty'; }

# Plans, refuses any plan that deletes or replaces a resource, then applies
# exactly that plan. An empty image_uri or a renamed resource would otherwise
# take the site down; such changes need a person to apply them.
plan_apply() {
  tf plan -input=false -out=deploy.tfplan "$@"
  local deletes
  deletes=$(tf show -json deploy.tfplan | jq -r '.resource_changes[]? | select(.change.actions | index("delete")) | .address')
  if [[ -n $deletes ]]; then
    echo "::error::The plan deletes or replaces resources; apply it by hand after review:" >&2
    echo "$deletes" >&2
    exit 1
  fi
  tf apply -input=false deploy.tfplan
  rm -f infra/deploy.tfplan
}

tf init -input=false

# workflow_run events can complete out of order. Refuse to put back a commit
# that the deployed one already contains; redeploying the same one is allowed.
deployed=$(output release)
if [[ -n $deployed && $deployed != "$RELEASE" ]]; then
  git fetch --quiet origin "$deployed" 2>/dev/null || true
  if git merge-base --is-ancestor "$RELEASE" "$deployed" 2>/dev/null; then
    echo "::notice::$RELEASE is older than the deployed $deployed; skipping."
    exit 0
  fi
fi

# First deploy: create ECR and the buckets before there is an image to run.
# release stays unset until an image is live, so a failure after this step
# does not make older commits look superseded.
if [[ -z $(output repository_url) ]]; then
  plan_apply -var image_uri=
fi
repo=$(output repository_url)
spa_bucket=$(output spa_bucket)
repo_name=${repo#*/}
aws ecr get-login-password | docker login --username AWS --password-stdin "${repo%%/*}"

# Tags are immutable, so a rerun reuses the image already pushed for this commit.
digest() {
  aws ecr describe-images --repository-name "$repo_name" --image-ids "imageTag=$RELEASE" \
    --query 'imageDetails[0].imageDigest' --output text 2>/dev/null
}
if ! image_digest=$(digest); then
  docker buildx build --platform linux/arm64 --provenance=false -t "$repo:$RELEASE" --push .
  image_digest=$(digest)
fi
image="$repo@$image_digest"
echo "image: $image"

# The share page is rendered from the index.html inside the image, so the
# static files must come from that same image, never from a local web/dist.
docker pull --quiet --platform linux/arm64 "$image" >/dev/null
cid=$(docker create --platform linux/arm64 "$image")
rm -rf release-web && mkdir release-web
docker cp "$cid:/app/web/." release-web
docker rm "$cid" >/dev/null

# Hashed assets go up first: adding files is harmless to the running version,
# and the new Lambda's share pages reference them as soon as it is live. Old
# assets are kept, since cached HTML may still reference them.
aws s3 sync release-web/assets "s3://$spa_bucket/assets/" --size-only \
  --cache-control 'public,max-age=31536000,immutable'

# Keep the URL the Lambda already has (a custom domain stays). Only the first
# deploy's first apply leaves the localhost default, which the CloudFront
# domain then replaces.
if [[ -z $BASE_URL ]]; then
  BASE_URL=$(output configured_base_url)
  if [[ $BASE_URL == http://localhost* ]]; then
    BASE_URL=$(output base_url)
  fi
fi
# The previous release marker is kept until the static files are published and
# the smoke test passes, so a run that fails partway never makes older commits
# look superseded.
args=(-var "image_uri=$image" -var "generation_enabled=$GENERATION_ENABLED")
if [[ -z $BASE_URL ]]; then
  # The CloudFront domain exists only after the first apply; until the second
  # apply sets it, share links point at localhost and must not be published.
  plan_apply "${args[@]}" -var "release=$deployed"
  BASE_URL=$(output base_url)
fi
args+=(-var "base_url=$BASE_URL")
plan_apply "${args[@]}" -var "release=$deployed"

aws s3 cp release-web/kanji-ui.otf "s3://$spa_bucket/kanji-ui.otf" --cache-control 'public,max-age=86400'
aws s3 cp release-web/index.html "s3://$spa_bucket/index.html" --cache-control 'no-cache'
assets=()
while IFS= read -r key; do assets+=("$key"); done < <(grep -o '/assets/[A-Za-z0-9._-]*' release-web/index.html | sort -u)
if [[ ${#assets[@]} -eq 0 ]]; then
  echo "index.html references no /assets/ files" >&2
  exit 1
fi
for key in "${assets[@]}"; do
  aws s3api head-object --bucket "$spa_bucket" --key "${key#/}" >/dev/null
done

distribution=$(output distribution_id)
invalidation=$(aws cloudfront create-invalidation --distribution-id "$distribution" \
  --paths / /index.html '/g/*' /kanji-ui.otf --query 'Invalidation.Id' --output text)
aws cloudfront wait invalidation-completed --distribution-id "$distribution" --id "$invalidation"

# Smoke test through CloudFront. Nothing here writes a glyph.
expect() { # status url [grep-pattern]
  local body status
  for attempt in 1 2 3 4 5 6; do
    body=$(mktemp)
    status=$(curl -sS -o "$body" -w '%{http_code}' "$2") || status=000
    if [[ $status == "$1" ]] && { [[ -z ${3:-} ]] || grep -q -- "$3" "$body"; }; then
      rm -f "$body"
      echo "ok $1 $2"
      return 0
    fi
    rm -f "$body"
    sleep 10
  done
  echo "::error::expected $1${3:+ containing $3} from $2, last got $status" >&2
  return 1
}
expect 200 "$BASE_URL/" "${assets[0]}"
expect 200 "$BASE_URL${assets[0]}"
expect 200 "$BASE_URL/kanji-ui.otf"
expect 400 "$BASE_URL/api/v1/glyphs/not-an-id" '"code":"invalid_id"'
expect 404 "$BASE_URL/g/00000000000000000000000000000000" "${assets[0]}"
# The Function URL takes only CloudFront's signed requests.
expect 403 "$(output function_url)healthz"

# Everything is live; only now record this commit. The plan changes the output
# alone, so no resource is touched.
plan_apply "${args[@]}" -var "release=$RELEASE"
echo "deployed $RELEASE to $BASE_URL"
