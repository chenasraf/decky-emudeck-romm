import { FC, createElement, useEffect, useState } from "react";
import { getFrontendUnsupported, onFrontendUnsupportedChange } from "../utils/connectionState";
import { WarningCard } from "./WarningCard";
import type { FrontendUnsupportedPayload } from "../types";

/**
 * Subscribe to frontend-host unsupported-version state changes.
 * Returns the current payload (or null when the frontend's version sits
 * inside the plugin's tested band).
 */
export function useFrontendUnsupported(): FrontendUnsupportedPayload | null {
  const [payload, setPayload] = useState<FrontendUnsupportedPayload | null>(getFrontendUnsupported());
  useEffect(() => onFrontendUnsupportedChange(setPayload), []);
  return payload;
}

function formatMessage(payload: FrontendUnsupportedPayload): string {
  const detected = payload.detected ?? "unknown";
  return (
    `${payload.frontend} ${detected} is outside the plugin's tested range ` +
    `[${payload.expected_min}, ${payload.expected_max}]. ` +
    `Update ${payload.frontend} or open an issue with your version reported.`
  );
}

interface FrontendUnsupportedCardProps {
  payload: FrontendUnsupportedPayload;
  /** Compact mode for narrow contexts (QAM panel). */
  compact?: boolean;
}

/**
 * Polished error card shown when the host emulator-frontend
 * (RetroDECK / EmuDeck) reports a version outside the plugin's
 * tested band — the backend refuses to wire services in this state,
 * so every sync-affecting CTA must be hidden until the user updates.
 */
export const FrontendUnsupportedCard: FC<FrontendUnsupportedCardProps> = ({ payload, compact = false }) =>
  createElement(WarningCard, {
    title: `${payload.frontend} version not supported`,
    message: formatMessage(payload),
    compact,
  });
