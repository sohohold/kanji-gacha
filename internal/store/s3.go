package store

import (
	"bytes"
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"io"
)

type S3 struct {
	Client *s3.Client
	Bucket string
}

func NewS3(ctx context.Context, bucket, endpoint string) (*S3, error) {
	cfg, e := awsconfig.LoadDefaultConfig(ctx)
	if e != nil {
		return nil, e
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}
	})
	return &S3{client, bucket}, nil
}
func (s *S3) Get(ctx context.Context, k string) ([]byte, error) {
	if e := checkKey(k); e != nil {
		return nil, e
	}
	o, e := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.Bucket, Key: &k})
	if e != nil {
		return nil, classify(e)
	}
	defer o.Body.Close()
	return io.ReadAll(io.LimitReader(o.Body, 16<<20))
}
func (s *S3) PutIfAbsent(ctx context.Context, k string, b []byte, ct string) error {
	if e := checkKey(k); e != nil {
		return e
	}
	_, e := s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.Bucket, Key: &k, Body: bytes.NewReader(b), ContentType: &ct, IfNoneMatch: aws.String("*")})
	return classify(e)
}
func classify(e error) error {
	var ae smithy.APIError
	if errors.As(e, &ae) {
		switch ae.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return ErrNotFound
		case "PreconditionFailed", "ConditionalRequestConflict":
			return ErrExists
		}
	}
	return e
}
