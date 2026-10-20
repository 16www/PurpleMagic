import React from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import "./origin.css";
import "./app.css";
import "./purple.css";
import "./responsive.css";
import "./live.css";
import "./motion.css";
import "./brand.css";

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
