export interface PagerEmptyState {
  title: string
  body: string
  /** Which capcode screen the call to action opens, if any. */
  action?: "select" | "adjust"
}

/**
 * Copy for the pager feed when it has nothing to show. A filtered feed is
 * empty when the filtered capcodes have had no pages at all, which is easy to
 * mistake for the filter being off, so say what was filtered and why it is
 * empty.
 */
export interface PagerFilterState {
  filterOn: boolean
  filterCount: number
  /** False until the saved filter list has been fetched; an empty list
   *  before then means "unknown", not "none selected". */
  filtersLoaded?: boolean
}

/**
 * Whether to blank the feed because filtering is on with nothing selected.
 * The server already filters the feed, so this only exists to show the
 * "select some capcodes" state, and it must wait for the list to load:
 * blanking early hid a correctly filtered feed on every cold start.
 */
export function hideFeedForEmptyFilter({
  filterOn,
  filterCount,
  filtersLoaded = true,
}: PagerFilterState): boolean {
  return filterOn && filtersLoaded && filterCount === 0
}

export function pagerEmptyState({
  filterOn,
  filterCount,
  filtersLoaded = true,
}: PagerFilterState): PagerEmptyState {
  if (filterOn && !filtersLoaded) {
    return { title: "No Messages", body: "There are no messages to show yet." }
  }
  if (filterOn && filterCount === 0) {
    return {
      title: "No Capcodes Selected",
      body: "You have filtering enabled but no capcodes selected.",
      action: "select",
    }
  }
  if (filterOn) {
    const subject =
      filterCount === 1
        ? "Your filtered capcode hasn't"
        : `None of your ${filterCount} filtered capcodes have`
    return {
      title: "No Pages Yet",
      body: `${subject} had a page come through our receivers yet. Turn filtering off to see everything else.`,
      action: "adjust",
    }
  }
  return { title: "No Messages", body: "There are no messages to show yet." }
}
