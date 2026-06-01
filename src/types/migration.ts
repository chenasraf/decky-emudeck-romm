/**
 * RetroArch save-sort migration types — the pending-migration status and
 * the result shape returned after running a save-sort migration. The
 * RetroDECK home-path migration was removed when the fork retargeted to
 * EmuDeck-only paths; only the save-sort flow remains.
 */

interface ConflictDetail {
  filename: string;
  old_path: string;
  old_size: number;
  old_mtime: string;
  new_path: string;
  new_size: number;
  new_mtime: string;
}

export interface MigrationResult {
  success: boolean;
  message: string;
  needs_confirmation?: boolean;
  conflict_count?: number;
  conflicts?: string[] | ConflictDetail[];
  saves_moved?: number;
  errors?: string[];
}

export interface SaveSortMigrationStatus {
  pending: boolean;
  old_settings?: { sort_by_content: boolean; sort_by_core: boolean };
  new_settings?: { sort_by_content: boolean; sort_by_core: boolean };
  saves_count?: number;
}
