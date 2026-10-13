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
export function pagerEmptyState({
  filterOn,
  filterCount,
}: {
  filterOn: boolean
  filterCount: number
}): PagerEmptyState {
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
