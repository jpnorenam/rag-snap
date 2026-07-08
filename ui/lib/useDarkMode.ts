"use client";

import { useEffect, useState } from "react";

// useDarkMode mirrors rag-snap-ui's dark-mode handling: a boolean persisted in
// localStorage that toggles Vanilla Framework's `is-dark` class on <html>.
export function useDarkMode(): [boolean, () => void] {
  const [darkMode, setDarkMode] = useState(false);

  // Load the persisted preference once on mount.
  useEffect(() => {
    if (localStorage.getItem("darkMode") === "true") setDarkMode(true);
  }, []);

  // Keep the <html> class in sync with the state.
  useEffect(() => {
    const root = document.documentElement;
    if (darkMode) root.classList.add("is-dark");
    else root.classList.remove("is-dark");
  }, [darkMode]);

  function toggle() {
    setDarkMode((d) => {
      localStorage.setItem("darkMode", String(!d));
      return !d;
    });
  }

  return [darkMode, toggle];
}
