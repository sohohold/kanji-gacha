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
	"time"
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
	// S3 answers 412 PreconditionFailed once the key exists, and 409
	// ConditionalRequestConflict while a competing write to it is still in
	// progress. The 409 says nothing about whether that write will land, so the
	// PUT is retried until it either wins or meets the finished object as a 412.
	for delay := 25 * time.Millisecond; ; delay *= 2 {
		_, e := s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: &s.Bucket, Key: &k, Body: bytes.NewReader(b), ContentType: &ct, IfNoneMatch: aws.String("*")})
		if code(e) != "ConditionalRequestConflict" || delay > 200*time.Millisecond {
			return classify(e)
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func code(e error) string {
	var ae smithy.APIError
	if errors.As(e, &ae) {
		return ae.ErrorCode()
	}
	return ""
}
func classify(e error) error {
	switch code(e) {
	case "NoSuchKey", "NotFound":
		return ErrNotFound
	case "PreconditionFailed", "ConditionalRequestConflict":
		return ErrExists
	}
	return e
}
