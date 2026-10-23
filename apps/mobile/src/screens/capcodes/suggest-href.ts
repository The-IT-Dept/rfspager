export interface SuggestTarget {
  capcode_id?: number | null
  address: string
  alias?: string | null
  agency?: string | null
}

/**
 * Route to the suggestion form. A target with a capcode_id opens it as an
 * amendment to that capcode, prefilled with its current alias and agency.
 */
export function suggestCapcodeHref(t: SuggestTarget): `/capcodes/suggest?${string}` {
  const qs = new URLSearchParams({ address: t.address })
  if (t.capcode_id) qs.set("capcode_id", String(t.capcode_id))
  if (t.alias) qs.set("alias", t.alias)
  if (t.agency) qs.set("agency", t.agency)
  return `/capcodes/suggest?${qs.toString()}`
}
