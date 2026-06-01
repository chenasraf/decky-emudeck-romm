/**
 * Connection, settings, and ROM-lookup types — the backend surface that
 * isn't specific to one feature vertical. Things that talk to RomM at the
 * connection/auth/metadata layer live here; per-domain shapes
 * (sync, saves, firmware, downloads, achievements) live in their own files.
 */

export type RommErrorCode =
  | "auth_error"
  | "forbidden_error"
  | "connection_error"
  | "timeout_error"
  | "ssl_error"
  | "server_error"
  | "not_found_error"
  | "unsupported_error"
  | "version_error"
  | "version_unsupported"
  | "config_error"
  | "disk_error"
  | "api_error"
  | "stale_conflict"
  | "stale_preview"
  | "unknown_error";

/**
 * Additive payload carried alongside ``error_code: "version_unsupported"``
 * when the backend's chosen emulator-frontend (RetroDECK / EmuDeck) is
 * outside the plugin's tested version band. Populated by the
 * connection-check callable when bootstrap raised
 * ``FrontendUnsupportedError``.
 */
export interface FrontendUnsupportedPayload {
  frontend: string;
  detected: string | null;
  expected_min: string;
  expected_max: string;
}

export interface InstalledRom {
  rom_id: number;
  file_name: string;
  file_path: string;
  system: string;
  platform_slug: string;
  installed_at: string;
}

export interface RetroArchInputCheck {
  warning: boolean;
  current?: string;
  config_path?: string;
}

export interface PluginSettings {
  romm_url: string;
  romm_user: string;
  romm_pass_masked: string;
  has_credentials: boolean;
  steam_input_mode: "default" | "force_on" | "force_off";
  sgdb_api_key_masked: string;
  log_level: "debug" | "info" | "warn" | "error";
  romm_allow_insecure_ssl: boolean;
  retroarch_input_check?: RetroArchInputCheck;
  collection_create_platform_groups?: boolean;
}

export interface RomMetadata {
  summary: string;
  genres: string[];
  companies: string[];
  first_release_date: number | null;
  average_rating: number | null;
  game_modes: string[];
  player_count: string;
  cached_at: number;
  steam_categories?: number[];
}

export interface LaunchVerdict {
  action: "allow" | "warn" | "block";
  reason: "not_installed" | "save_conflict" | "save_status_failed" | null;
  toast_title: string | null;
  toast_body: string | null;
}
