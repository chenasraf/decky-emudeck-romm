import { useEffect, useState } from "react";

/**
 * Returns ``value`` debounced by ``delay`` ms. The returned value lags the
 * input by ``delay`` ms of stillness; rapid changes coalesce.
 *
 * Used by the Library tab's search box so each keystroke doesn't fire a
 * ``browse_roms`` round-trip.
 */
export function useDebounce<T>(value: T, delay = 300): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const handle = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(handle);
  }, [value, delay]);

  return debounced;
}
