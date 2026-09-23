package main

import (
	"context"
	"kanji-gacha/config"
	"kanji-gacha/internal/compose"
	"kanji-gacha/internal/dict"
	"kanji-gacha/internal/httpapi"
	"kanji-gacha/internal/metrics"
	"kanji-gacha/internal/store"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func main() {
	c, e := config.Load(os.Getenv("GENERATOR_CONFIG"))
	if e != nil {
		log.Fatal(e)
	}
	d, e := dict.Load()
	if e != nil {
		log.Fatal(e)
	}
	var st store.Store = store.Local{Dir: env("DATA_DIR", ".local/data")}
	if bucket := os.Getenv("S3_BUCKET"); bucket != "" {
		st, e = store.NewS3(context.Background(), bucket, os.Getenv("S3_ENDPOINT"))
		if e != nil {
			log.Fatal(e)
		}
	}
	app, e := httpapi.New(compose.New(c, d), st, env("BASE_URL", "http://localhost:8080"), env("WEB_DIR", "web/dist"))
	if e != nil {
		log.Fatal(e)
	}
	app.GenerationEnabled = os.Getenv("GENERATION_ENABLED") != "false"
	app.TrustCloudFront = os.Getenv("TRUST_CLOUDFRONT") == "true"
	// EMF records go to stdout as bare JSON lines; log.Printf writes to stderr
	// with a date prefix, which would stop CloudWatch reading them as metrics.
	if ns := os.Getenv("METRICS_NAMESPACE"); ns != "" {
		app.Metrics = metrics.NewEMF(os.Stdout, ns)
	}
	srv := &http.Server{Addr: env("LISTEN_ADDR", ":8080"), Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()
	log.Printf("listening on %s; dictionary %s (%d parts)", srv.Addr, d.Version, len(d.Parts))
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
