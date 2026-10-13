import type { DisabledFeature, Feature } from './rpc/schema.gen'

const LABELS: Record<Feature, string> = {
  browse: 'Library browsing',
  download: 'ROM downloads',
  collections: 'Collections',
  bios: 'BIOS downloads',
  save_sync: 'Save sync',
}

export function featureLabel(feature: Feature): string {
  return LABELS[feature] ?? feature
}

/** Why a feature is off, for the user: names the permissions RomM didn't grant. */
export function disabledReason(d: DisabledFeature): string {
  return `RomM didn't grant ${d.missingScopes.join(', ')}. Sign in again and allow them to turn this on.`
}
