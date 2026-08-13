// @ts-check

/**
 * Builds the canonical patient URL for a search transition.
 *
 * A non-empty search deliberately drops patient context. An empty search keeps
 * a patient that the user explicitly selected from earlier search results.
 *
 * @param {string | URL} action
 * @param {{search: string, status: string, patientID: string}} state
 * @returns {URL}
 */
export function patientSearchURL(action, state) {
  const url = new URL(action)
  const search = state.search.trim()
  const status = state.status.trim()
  const patientID = state.patientID.trim()

  url.search = ''
  if (search) url.searchParams.set('q', search)
  if (status) url.searchParams.set('status', status)
  if (!search && patientID) url.searchParams.set('patient', patientID)
  return url
}
