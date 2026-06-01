import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useDebounce } from "./useDebounce";

describe("useDebounce", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns the initial value synchronously", () => {
    const { result } = renderHook(() => useDebounce("hello", 200));
    expect(result.current).toBe("hello");
  });

  it("delays propagation by the configured timeout", () => {
    const { result, rerender } = renderHook(({ v }: { v: string }) => useDebounce(v, 300), {
      initialProps: { v: "first" },
    });
    rerender({ v: "second" });
    expect(result.current).toBe("first");
    act(() => {
      vi.advanceTimersByTime(299);
    });
    expect(result.current).toBe("first");
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current).toBe("second");
  });

  it("coalesces rapid updates onto the last value", () => {
    const { result, rerender } = renderHook(({ v }: { v: string }) => useDebounce(v, 200), {
      initialProps: { v: "a" },
    });
    rerender({ v: "b" });
    act(() => vi.advanceTimersByTime(100));
    rerender({ v: "c" });
    act(() => vi.advanceTimersByTime(100));
    expect(result.current).toBe("a");
    act(() => vi.advanceTimersByTime(100));
    expect(result.current).toBe("c");
  });
});
