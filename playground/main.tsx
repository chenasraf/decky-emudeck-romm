import "./mocks/steam-globals";
import { createRoot } from "react-dom/client";
import { Harness } from "./Harness";

const root = createRoot(document.getElementById("root")!);
root.render(<Harness />);
