import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  QueryClient,
  QueryClientProvider,
  useMutation,
  useQuery,
} from "@tanstack/react-query";
import { APIError, generate, getGlyph, type Glyph } from "./api";
import { validate, issueMessage, type Input } from "./kana";
import "98.css";
import "./style.css";
const client = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: Infinity },
    mutations: { retry: false },
  },
});
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
function routeOf(path: string) {
  const id = path.match(/^\/g\/([0-9a-f]{32})$/)?.[1];
  return { id, invalid: path.startsWith("/g/") && !id };
}
function App() {
  const [path, setPath] = useState(location.pathname);
  const [mode, setMode] = useState<Input["mode"]>("kun");
  const [reading, setReading] = useState("");
  const [okurigana, setOkurigana] = useState("");
  const [composing, setComposing] = useState(false);
  const [touched, setTouched] = useState(false);
  const [notice, setNotice] = useState("");
  const [animate, setAnimate] = useState("");
  const [busy, setBusy] = useState(false);
  const card = useRef<HTMLDivElement>(null);
  const composingRef = useRef(false);
  const route = routeOf(path);
  const saved = useQuery({
    queryKey: ["glyph", route.id],
    queryFn: () => getGlyph(route.id!),
    enabled: !!route.id,
  });
  const mutation = useMutation({ mutationFn: generate });
  const g = saved.data;
  const input: Input = {
    mode,
    reading,
    okurigana: mode === "kun" ? okurigana : "",
  };
  const issue = !composing ? validate(input) : undefined;
  function go(next: string) {
    history.pushState(null, "", next);
    setPath(next);
    setNotice("");
    window.scrollTo(0, 0);
  }
  useEffect(() => {
    const sync = () => {
      setPath(location.pathname);
      setNotice("");
    };
    addEventListener("popstate", sync);
    return () => removeEventListener("popstate", sync);
  }, []);
  async function roll(e: React.FormEvent) {
    e.preventDefault();
    if (composingRef.current || busy) return;
    setTouched(true);
    if (validate(input)) return;
    setBusy(true);
    const start = performance.now();
    let made: Glyph | undefined;
    try {
      made = await mutation.mutateAsync(input);
    } catch {
    } finally {
      const rest = Math.max(0, 400 - (performance.now() - start));
      await new Promise((r) => setTimeout(r, rest));
      setBusy(false);
    }
    if (made) {
      client.setQueryData(["glyph", made.id], made);
      setAnimate(made.id);
      go(`/g/${made.id}`);
    }
  }
  async function share(url: string) {
    try {
      if (navigator.share)
        await navigator.share({ title: "架空漢字ガチャ", url });
      else {
        await navigator.clipboard.writeText(url);
        setNotice("リンクをコピーしました。");
      }
    } catch (e) {
      if ((e as Error).name !== "AbortError")
        setNotice(
          "リンクをコピーできませんでした。URLを選択してご利用ください。",
        );
    }
  }
  useEffect(() => {
    if (
      !g ||
      g.id !== animate ||
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
  }, [animate, g?.id]);
  const inResult = !!route.id || route.invalid;
  const url = g ? new URL(`/g/${g.id}`, location.origin).href : "";
  const hits = g
    ? [
        ...new Set(
          g.components.filter((c) => c.role === "reading").map((c) => c.char),
        ),
      ].filter(Boolean)
    : [];
  return (
    <main className="desktop">
      <div className={`window ${inResult ? "result" : "home"}`}>
        <div className="title-bar">
          <div className="title-bar-text">
            {inResult && g ? `${g.reading.display} - ` : ""}架空漢字ガチャ
          </div>
          {inResult && (
            <div className="title-bar-controls">
              <button
                aria-label="Close"
                title="ホームへ戻る"
                onClick={() => go("/")}
              />
            </div>
          )}
        </div>
        <div className="window-body">
          {inResult ? (
            <>
              {g ? (
                <>
                  <figure className="canvas sunken" ref={card}>
                    <div
                      className="glyph"
                      role="img"
                      aria-label={`${g.reading.display}の架空漢字 ${g.ids}`}
                      dangerouslySetInnerHTML={{ __html: g.svg }}
                    />
                    <figcaption>{g.reading.display}</figcaption>
                  </figure>
                  <fieldset>
                    <legend>共有リンク</legend>
                    <div className="field-row">
                      <input
                        className="share-url"
                        aria-label="共有リンク"
                        readOnly
                        value={url}
                        onFocus={(e) => e.target.select()}
                      />
                      <button type="button" onClick={() => share(url)}>
                        共有
                      </button>
                    </div>
                    <p className="notice" role="status">
                      {notice}
                    </p>
                  </fieldset>
                  {hits.length > 0 && (
                    <fieldset className="hits">
                      <legend>ヒットした漢字</legend>
                      <ul>
                        {hits.map((c) => (
                          <li key={c} className="sunken">
                            {c}
                          </li>
                        ))}
                      </ul>
                    </fieldset>
                  )}
                </>
              ) : (
                <p className="error" role="alert">
                  {route.invalid
                    ? "共有URLを確認してください。"
                    : saved.error
                      ? message(saved.error)
                      : "読み込み中…"}
                </p>
              )}
              <button type="button" className="again" onClick={() => go("/")}>
                もう一度回す
              </button>
            </>
          ) : (
            <>
              <h1>架空漢字ガチャ</h1>
              <form onSubmit={roll} noValidate>
                <menu role="tablist" aria-label="読みの種類">
                  {(["on", "kun"] as const).map((m) => (
                    <li key={m} role="tab" aria-selected={mode === m}>
                      <a
                        href="#"
                        onClick={(e) => {
                          e.preventDefault();
                          setMode(m);
                          setTouched(false);
                          mutation.reset();
                        }}
                      >
                        {m === "on" ? "音読み" : "訓読み"}
                      </a>
                    </li>
                  ))}
                </menu>
                <div className="window tabpanel" role="tabpanel">
                  <div className="window-body">
                    <div className="fields">
                      <input
                        id="reading"
                        type="text"
                        aria-label={mode === "kun" ? "読み（語幹）" : "読み"}
                        autoComplete="off"
                        spellCheck={false}
                        placeholder={mode === "on" ? "セイ" : "あお"}
                        value={reading}
                        aria-invalid={!!(touched && issue?.field === "reading")}
                        aria-describedby="input-error"
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
                        <input
                          id="okurigana"
                          type="text"
                          aria-label="送り仮名"
                          autoComplete="off"
                          placeholder="い"
                          value={okurigana}
                          onChange={(e) => {
                            setOkurigana(e.target.value);
                            mutation.reset();
                          }}
                          aria-invalid={
                            !!(touched && issue?.field === "okurigana")
                          }
                          onCompositionStart={() => {
                            composingRef.current = true;
                            setComposing(true);
                          }}
                          onCompositionEnd={() => {
                            composingRef.current = false;
                            setComposing(false);
                          }}
                        />
                      )}
                    </div>
                    <p id="input-error" className="error" role="alert">
                      {touched && issue
                        ? issueMessage(issue)
                        : mutation.error
                          ? message(mutation.error)
                          : ""}
                    </p>
                    {busy ? (
                      <div
                        className="progress-indicator segmented"
                        role="progressbar"
                        aria-label="生成中"
                      >
                        <span className="progress-indicator-bar" />
                      </div>
                    ) : (
                      <button
                        className="roll default"
                        type="submit"
                        disabled={composing}
                      >
                        ガチャを回す
                      </button>
                    )}
                  </div>
                </div>
              </form>
            </>
          )}
        </div>
        <footer className="status-bar">
          <p className="status-bar-field">
            <span>
              <a href="https://kanjivg.tagaini.net/">KanjiVG</a> © Ulrich Apel
              (改変,{" "}
              <a href="https://creativecommons.org/licenses/by-sa/3.0/">
                CC BY-SA 3.0
              </a>
              )
            </span>{" "}
            /{" "}
            <span>
              <a href="https://www.edrdg.org/wiki/index.php/KANJIDIC_Project">
                KANJIDIC2
              </a>{" "}
              © EDRDG (
              <a href="https://www.edrdg.org/edrdg/licence.html">
                CC BY-SA 4.0
              </a>
              )
            </span>{" "}
            /{" "}
            <span>
              <a href="https://github.com/notofonts/noto-cjk">Noto CJK</a> ©
              Adobe (<a href="https://openfontlicense.org/">OFL 1.1</a>)
            </span>
          </p>
        </footer>
      </div>
    </main>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={client}>
      <App />
    </QueryClientProvider>
  </React.StrictMode>,
);
