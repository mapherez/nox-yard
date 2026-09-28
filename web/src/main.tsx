import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@phosphor-icons/web/bold/style.css";
import "@phosphor-icons/web/fill/style.css";
import App from "./App";
import "./styles/tokens.css";
import "./styles/global.css";

const root = document.getElementById("root");
if (!root) {
  throw new Error("Application root is missing.");
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
