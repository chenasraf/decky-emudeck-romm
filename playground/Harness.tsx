import { useEffect, useState } from "react";
import { demos } from "./demos";

function getInitialDemo(): string {
  const hash = new URLSearchParams(window.location.hash.slice(1));
  const requested = hash.get("demo");
  if (requested && demos[requested]) return requested;
  return Object.keys(demos)[0] ?? "";
}

export function Harness() {
  const [active, setActive] = useState(getInitialDemo);

  useEffect(() => {
    window.location.hash = `demo=${active}`;
  }, [active]);

  const Demo = demos[active];

  return (
    <div style={{ display: "grid", gridTemplateColumns: "220px 1fr", minHeight: "100vh" }}>
      <aside style={{ background: "#171a21", padding: 16, borderRight: "1px solid #2a3548" }}>
        <h3 style={{ margin: "0 0 12px", fontSize: 13, textTransform: "uppercase", letterSpacing: 1, color: "#66c0f4" }}>
          Demos
        </h3>
        <nav style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          {Object.keys(demos).map((name) => (
            <button
              key={name}
              onClick={() => setActive(name)}
              style={{
                textAlign: "left",
                padding: "6px 8px",
                background: name === active ? "#2a475e" : "transparent",
                color: "#c7d5e0",
                border: "none",
                borderRadius: 2,
                cursor: "pointer",
                fontSize: 14,
              }}
            >
              {name}
            </button>
          ))}
        </nav>
        <p style={{ marginTop: 24, fontSize: 11, color: "#7a8a9d", lineHeight: 1.4 }}>
          Add demos in <code>playground/demos/</code> and register in
          <code> demos/index.tsx</code>. Backend callables and toasts log to
          the devtools console.
        </p>
      </aside>
      <main style={{ padding: 24 }}>
        <h2 style={{ marginTop: 0, color: "#fff" }}>{active}</h2>
        <div style={{ background: "#1e3346", padding: 16, borderRadius: 4 }}>
          {Demo ? <Demo /> : <p>No demos registered.</p>}
        </div>
      </main>
    </div>
  );
}
