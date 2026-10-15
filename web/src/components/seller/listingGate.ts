import { isTested, type DetectionStatus } from '@/api/market-detection'

export function sellerUntested(row?: { detection_status?: DetectionStatus; is_reference?: boolean } | null): boolean {
  if (!row || row.is_reference) return false
  return !isTested(row.detection_status)
}

export function privacyBlocksListing(row?: { privacy_ok?: boolean } | null): boolean {
  return row?.privacy_ok === false
}
