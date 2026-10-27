import { StrictMode, useState } from "react";
import { createRoot } from "react-dom/client";
import { useRun } from "./useRun";
import "./style.css";

function App() {
  const [runId, setRunId] = useState(new URLSearchParams(location.search).get("run") ?? "");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const { state, status } = useRun(runId);

  async function post(resume: boolean) {
    setBusy(true);
    setError("");
    try {
      const response = await fetch(
        resume ? `/api/runs/${encodeURIComponent(runId)}/resume` : "/api/runs",
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ question: "What is the status of order-1001?" }),
        },
      );
      const body = await response.json();
      if (!response.ok) throw new Error(body.error);
      const url = new URL(location.href);
      url.searchParams.set("run", body.runId);
      history.replaceState(null, "", url);
      // A resumed run uses the same ID. Reload to start the same catch-up protocol again.
      if (resume) location.reload();
      else setRunId(body.runId);
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main>
      <p className="eyebrow">GENT / STREAMING EXAMPLE</p>
      <h1>An answer you can refresh.</h1>
      <p>Start a run, then refresh this page while the preview arrives.</p>
      <div className="actions">
        <button disabled={busy} onClick={() => void post(false)}>
          Start order lookup
        </button>
        <button disabled={busy || !runId} onClick={() => void post(true)}>
          Resume saved run
        </button>
      </div>
      {error && <p role="alert">{error}</p>}
      <p role="status">{status}</p>
      {state && (
        <>
          <p>
            Run <code>{runId}</code> · Event <code>{state.lastEventNumber}</code>
          </p>
          <p>Run status: {state.status}</p>
          {state.previews.map((preview) => {
            const replaced = new Set(
              state.answers
                .filter((answer) => answer.contextId === preview.contextId)
                .flatMap((answer) => answer.sourceMessageIds ?? []),
            );
            const parts = preview.parts.filter((part) => !replaced.has(part.messageId));
            if (!parts.length) return null;
            return (
              <section key={`${preview.id}/${preview.attemptId}`} aria-label="Model preview">
                <h2>Preview · {preview.status}</h2>
                {parts.map((part, index) => (
                  <p key={index}>
                    <small>{part.type}</small>
                    <br />
                    {part.text}
                  </p>
                ))}
              </section>
            );
          })}
          {state.answers.map((answer) => (
            <section key={answer.contextId} aria-label="Accepted answer">
              <h2>Accepted answer</h2>
              <p>{answer.text || "(empty answer)"}</p>
            </section>
          ))}
        </>
      )}
    </main>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
