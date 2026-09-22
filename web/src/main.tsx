import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
  useQuery,
} from "@tanstack/react-query";
import { APIError, generate, getGlyph, type Glyph } from "./api";
import { normalize, validate, issueMessage, type Input } from "./kana";
import "./style.css";
const client = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: Infinity },
    mutations: { retry: false },
  },
});
function historyRead(): string[] {
  try {
    const xs: unknown = JSON.parse(
      localStorage.getItem("kanji-history") ?? "[]",
    );
    return Array.isArray(xs)
      ? xs
          .filter(
            (s): s is string =>
              typeof s === "string" && /^[0-9a-f]{32}$/.test(s),
          )
          .slice(0, 20)
      : [];
  } catch {
    return [];
  }
}
function message(e: unknown): string {
  if (e instanceof APIError) {
    const p = e.problem;
    if (p.errors?.[0]) return issueMessage(p.errors[0]);
    if (p.status === 429) return "少し待ってから、もう一度回してください。";
    if (p.status === 404) return "生成結果が見つかりませんでした。";
    if (p.status === 400) return "共有URLまたは入力内容を確認してください。";
    if (p.status === 405) return "この操作には対応していません。";
    if (p.status === 413) return "入力が長すぎます。短くしてお試しください。";
    if (p.status === 415)
      return "送信形式が不正です。ページを再読み込みしてください。";
    if (p.status === 422) return "入力内容を確認してください。";
    if (p.code === "generation_paused")
      return "ただいま生成をお休みしています。";
    if (p.status === 503)
      return "保存または生成に失敗しました。時間をおいてもう一度お試しください。";
  }
  return "接続できませんでした。通信環境をご確認ください。";
}
function App() {
  const [mode, setMode] = useState<Input["mode"]>("on");
  const [reading, setReading] = useState("");
  const [okurigana, setOkurigana] = useState("");
  const [composing, setComposing] = useState(false);
  const [touched, setTouched] = useState(false);
  const [ids, setIds] = useState(historyRead);
  const [active, setActive] = useState<Glyph>();
  const [notice, setNotice] = useState("");
  const [replay, setReplay] = useState(0);
  const [busy, setBusy] = useState(false);
  const card = useRef<HTMLDivElement>(null);
  const composingRef = useRef(false);
  const routeId = window.location.pathname.match(/^\/g\/([0-9a-f]{32})$/)?.[1];
  const invalidRoute = window.location.pathname.startsWith("/g/") && !routeId;
  const saved = useQuery({
    queryKey: ["glyph", routeId],
    queryFn: () => getGlyph(routeId!),
    enabled: !!routeId,
  });
  const mutation = useMutation({
    mutationFn: generate,
    onSuccess: (g) => {
      client.setQueryData(["glyph", g.id], g);
      setActive(g);
      setReplay(0);
      setIds((old) => {
        const next = [g.id, ...old.filter((x) => x !== g.id)].slice(0, 20);
        try {
          localStorage.setItem("kanji-history", JSON.stringify(next));
        } catch {}
        return next;
      });
    },
  });
  const g = active ?? saved.data;
  const input: Input = {
    mode,
    reading,
    okurigana: mode === "kun" ? okurigana : "",
  };
  const issue = !composing ? validate(input) : undefined;
  async function roll(e: React.FormEvent) {
    e.preventDefault();
    if (composingRef.current || busy) return;
    setTouched(true);
    setNotice("");
    if (validate(input)) return;
    setBusy(true);
    const start = performance.now();
    try {
      await mutation.mutateAsync(input);
    } catch {
    } finally {
      const rest = Math.max(0, 400 - (performance.now() - start));
      await new Promise((r) => setTimeout(r, rest));
      setBusy(false);
    }
  }
  async function share() {
    if (!g) return;
    const url = new URL(`/g/${g.id}`, location.origin).href;
    try {
      if (navigator.share)
        await navigator.share({ title: "架空漢字ガチャ", url });
      else {
        await navigator.clipboard.writeText(url);
        setNotice("リンクをコピーしました。");
      }
    } catch (e) {
      if ((e as Error).name !== "AbortError")
        setNotice("リンクをコピーできませんでした。下のURLをご利用ください。");
    }
  }
  // Records saved before the Biang slot was removed hold a filled outline with no stroke order.
  const replayable = g?.legend !== "biang";
  useEffect(() => {
    if (
      !g ||
      !replayable ||
      !replay ||
      matchMedia("(prefers-reduced-motion: reduce)").matches
    )
      return;
    const paths = card.current?.querySelectorAll<SVGPathElement>("path");
    const animations: Animation[] = [];
    paths?.forEach((path, i) => {
      const len = path.getTotalLength();
      animations.push(
        path.animate(
          [
            { strokeDasharray: `${len}`, strokeDashoffset: len },
            { strokeDasharray: `${len}`, strokeDashoffset: 0 },
          ],
          { duration: 180, delay: i * 130, fill: "both" },
        ),
      );
    });
    return () => animations.forEach((a) => a.cancel());
  }, [replay, g?.id]);
  return (
    <>
      <header className="topbar">
        <a href="/" className="brand">
          <span className="brand-mark">字</span>架空漢字ガチャ
        </a>
      </header>
      <main>
        <div className="intro">
          <h1>架空漢字ガチャ</h1>
          <p className="lead">
            読みを入力すると、漢字の部品を組み合わせて字形を生成します。
          </p>
        </div>
        <div className="workspace">
          <section className="controls">
            <div className="section-label">
              <span>01</span> 入力
            </div>
            <form onSubmit={roll} noValidate>
              <div className="tabs" role="tablist" aria-label="読みの種類">
                {(["on", "kun"] as const).map((m) => (
                  <button
                    key={m}
                    type="button"
                    role="tab"
                    aria-selected={mode === m}
                    onClick={() => {
                      setMode(m);
                      setTouched(false);
                      mutation.reset();
                    }}
                  >
                    {m === "on" ? "音読み" : "訓読み"}
                    <small>
                      {m === "on"
                        ? "セイ、トウキョウ"
                        : "あお（い）、はし（る）"}
                    </small>
                  </button>
                ))}
              </div>
              <label htmlFor="reading">
                {mode === "kun" ? "読み（語幹）" : "読み"}
                <span aria-hidden="true">
                  {[...normalize(reading)].length} / 5
                </span>
              </label>
              <input
                id="reading"
                autoComplete="off"
                spellCheck={false}
                placeholder={mode === "on" ? "例：セイ" : "例：あお"}
                value={reading}
                aria-invalid={!!(touched && issue?.field === "reading")}
                aria-describedby="input-help input-error"
                onChange={(e) => {
                  setReading(e.target.value);
                  mutation.reset();
                }}
                onBlur={() => setTouched(true)}
                onCompositionStart={() => {
                  composingRef.current = true;
                  setComposing(true);
                }}
                onCompositionEnd={() => {
                  composingRef.current = false;
                  setComposing(false);
                }}
              />
              {mode === "kun" && (
                <>
                  <label htmlFor="okurigana">
                    送り仮名
                    <span aria-hidden="true">
                      {[...normalize(okurigana)].length} / 3
                    </span>
                  </label>
                  <input
                    id="okurigana"
                    autoComplete="off"
                    placeholder="例：い（省略可）"
                    value={okurigana}
                    onChange={(e) => setOkurigana(e.target.value)}
                    aria-invalid={!!(touched && issue?.field === "okurigana")}
                    onCompositionStart={() => {
                      composingRef.current = true;
                      setComposing(true);
                    }}
                    onCompositionEnd={() => {
                      composingRef.current = false;
                      setComposing(false);
                    }}
                  />
                </>
              )}
              <p id="input-help" className="hint">
                ひらがな・カタカナで入力
                {mode === "kun"
                  ? "。語幹と送り仮名は合計6文字まで。"
                  : "。5文字まで。"}
              </p>
              <p id="input-error" className="error" role="alert">
                {touched && issue
                  ? issueMessage(issue)
                  : mutation.error
                    ? message(mutation.error)
                    : ""}
              </p>
              <button
                className="roll"
                type="submit"
                disabled={busy || composing}
              >
                {busy ? (
                  <>
                    <span className="spinner" /> 生成中…
                  </>
                ) : (
                  <>
                    ガチャを回す <span>↗</span>
                  </>
                )}
              </button>
            </form>
            <div className="rarity-guide">
              <span>レアリティ</span>
              <div>
                {["N", "R", "SR", "SSR", "UR"].map((r) => (
                  <b key={r} className={`rarity-${r}`}>
                    {r}
                  </b>
                ))}
              </div>
            </div>
          </section>
          <section
            className={`result ${busy ? "is-busy" : ""}`}
            aria-label="生成結果"
            aria-busy={busy}
          >
            <div className="card-top">
              <span>生成結果</span>
              {g && (
                <span className={`badge rarity-${g.rarity}`}>{g.rarity}</span>
              )}
            </div>
            <div className="paper" ref={card}>
              {g ? (
                <div
                  className="glyph"
                  dangerouslySetInnerHTML={{ __html: g.svg }}
                />
              ) : (
                <div className="empty-glyph">
                  <span>未</span>
                </div>
              )}
              <div className="paper-grid" />
              <span className="corner tl" />
              <span className="corner br" />
            </div>
            <div className="result-caption" aria-live="polite">
              {busy ? (
                <h2>生成中…</h2>
              ) : g ? (
                <>
                  <h2>{g.reading.display}</h2>
                  <p>
                    <span>{g.strokes}画</span>
                    <span className="ids">{g.ids}</span>
                  </p>
                </>
              ) : (
                <>
                  <h2>未生成</h2>
                  <p>読みを入力して「ガチャを回す」を押してください。</p>
                </>
              )}
            </div>
            {(saved.isLoading || saved.error || invalidRoute) && !active && (
              <p className="error" role="alert">
                {invalidRoute
                  ? "共有URLを確認してください。"
                  : saved.error
                    ? message(saved.error)
                    : "読み込み中…"}
              </p>
            )}
            {g && (
              <div className="result-actions">
                <button
                  onClick={() => setReplay((x) => x + 1)}
                  disabled={busy || !replayable}
                >
                  ▷ 描画を再生
                </button>
                <button className="share" onClick={share} disabled={busy}>
                  結果を共有 ↗
                </button>
                <a
                  href={`https://twitter.com/intent/tweet?url=${encodeURIComponent(new URL(`/g/${g.id}`, location.origin).href)}`}
                  target="_blank"
                  rel="noreferrer"
                  aria-label="Xでシェア"
                >
                  𝕏
                </a>
              </div>
            )}
            {g && (
              <a className="share-url" href={`/g/${g.id}`}>
                {location.origin}/g/{g.id}
              </a>
            )}
            <p className="notice" role="status">
              {notice}
            </p>
          </section>
        </div>
        {ids.length > 0 && (
          <section className="history">
            <div className="section-label">
              <span>03</span> 履歴 <small>このブラウザの直近20件</small>
            </div>
            <div className="history-grid">
              {ids.map((id) => (
                <HistoryItem
                  key={id}
                  id={id}
                  select={setActive}
                  active={g?.id === id}
                />
              ))}
            </div>
          </section>
        )}
      </main>
      <footer>
        <p>
          字形：<a href="https://kanjivg.tagaini.net/">KanjiVG © Ulrich Apel</a>
          （改変・
          <a href="https://creativecommons.org/licenses/by-sa/3.0/">
            CC BY-SA 3.0
          </a>
          ）<br />
          読み：
          <a href="https://www.edrdg.org/wiki/index.php/KANJIDIC_Project">
            KANJIDIC2 © EDRDG
          </a>
          （<a href="https://www.edrdg.org/edrdg/licence.html">CC BY-SA 4.0</a>
          ）<br />
          フォント：
          <a href="https://github.com/notofonts/noto-cjk">Noto CJK © Adobe</a>（
          <a href="https://openfontlicense.org/">SIL OFL 1.1</a>）
        </p>
        <details>
          <summary>使い方・生成確率</summary>
          <p>
            字の意味や実在性は判定しません。結果と入力した読みは共有URLから公開で閲覧できます。
          </p>
          <p>
            通常枠：N 70% / R 20% / SR 7% / SSR 2.5% / UR
            0.5%。特殊枠は別抽選。幽霊文字枠は0.3%、日本時間2:00〜2:30は3%。「タイト」は固定の特殊枠です。
          </p>
          <p>
            生成SVGはKanjiVGを再配置した改変物です。配布時は出典・変更内容・ライセンスを引き継いでください。保存はサービスの運用中に限り、無期限の提供を保証するものではありません。
          </p>
        </details>
      </footer>
    </>
  );
}
function HistoryItem({
  id,
  select,
  active,
}: {
  id: string;
  select: (g: Glyph) => void;
  active: boolean;
}) {
  const q = useQuery({ queryKey: ["glyph", id], queryFn: () => getGlyph(id) });
  return (
    <button
      className={`history-item ${active ? "selected" : ""}`}
      onClick={() => q.data && select(q.data)}
      disabled={!q.data}
      aria-label={
        q.data
          ? `${q.data.reading.display} ${q.data.rarity}を表示`
          : "履歴を読み込み中"
      }
    >
      {q.data ? (
        <>
          <div dangerouslySetInnerHTML={{ __html: q.data.svg }} />
          <span>{q.data.reading.display}</span>
          <small>{q.data.rarity}</small>
        </>
      ) : (
        <span>{q.error ? "取得できません" : "…"}</span>
      )}
    </button>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={client}>
      <App />
    </QueryClientProvider>
  </React.StrictMode>,
);
