/**
 * Browse-tab types — RomM ROM dicts as returned by the ``browse_roms`` callable.
 *
 * Only the fields the Library tab UI consumes are declared here. The backend
 * relays RomM's payload as-is, so additional fields surface as ``unknown``
 * extras — narrow them in the consumer when needed.
 */

export interface BrowseRom {
  id: number;
  name?: string;
  fs_name?: string;
  platform_id?: number;
  platform_slug?: string;
  platform_name?: string;
  path_cover_small?: string;
  path_cover_large?: string;
  url_cover?: string;
}

export interface BrowseRomsResult {
  success: boolean;
  items: BrowseRom[];
  total: number;
  message?: string;
  error_code?: string;
}
