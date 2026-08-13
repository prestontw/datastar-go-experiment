// @ts-check

const patientNavigationSelector = 'a[data-patient-navigation]'
const patientSearchSelector = 'form[data-patient-search]'
const patientSearchDelay = 300
/** @type {WeakMap<HTMLFormElement, number>} */
const patientSearchTimers = new WeakMap()

/**
 * Returns the progressively enhanced patient link for an event, if this is a
 * plain primary-button activation. Modified and non-primary activations retain
 * native link behavior (new tab/window, context menu, downloads, and so on).
 *
 * @param {MouseEvent} event
 * @returns {HTMLAnchorElement | null}
 */
function patientNavigationLink(event) {
  if (
    event.button !== 0 ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey ||
    !(event.target instanceof Element)
  ) {
    return null
  }

  const link = event.target.closest(patientNavigationSelector)
  if (
    !(link instanceof HTMLAnchorElement) ||
    link.hasAttribute('download') ||
    (link.target && link.target !== '_self')
  ) {
    return null
  }

  const url = new URL(link.href, document.baseURI)
  if (url.origin !== window.location.origin || url.pathname !== '/patients') {
    return null
  }
  return link
}

/**
 * @param {string} eventName
 * @param {URL} url
 */
function dispatchPatientURL(eventName, url) {
  const destination = `${url.pathname}${url.search}${url.hash}`
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`
  if (destination === current) return

  window.dispatchEvent(new CustomEvent(eventName, { detail: destination }))
}

/** @param {HTMLAnchorElement} link */
function navigateToPatientView(link) {
  dispatchPatientURL('patient-navigation', new URL(link.href, document.baseURI))
}

/** @param {HTMLFormElement} form */
function navigateToPatientSearch(form) {
  const data = new FormData(form)
  const url = new URL(form.action, document.baseURI)
  url.search = ''
  for (const [name, value] of data) {
    if (typeof value === 'string' && value.trim()) {
      url.searchParams.append(name, value.trim())
    }
  }
  dispatchPatientURL('patient-search', url)
}

/** @param {HTMLFormElement} form */
function schedulePatientSearch(form) {
  const timer = patientSearchTimers.get(form)
  if (timer) window.clearTimeout(timer)
  patientSearchTimers.set(form, window.setTimeout(() => {
    patientSearchTimers.delete(form)
    navigateToPatientSearch(form)
  }, patientSearchDelay))
}

// Mouse users begin the request on press rather than waiting for release. Do
// not prevent the mousedown default: the link should still receive focus.
document.addEventListener('mousedown', (event) => {
  const link = patientNavigationLink(event)
  if (link) navigateToPatientView(link)
})

// Suppress the native navigation after a handled mousedown. This also provides
// an accessible click fallback for keyboard activation and synthetic clicks.
document.addEventListener('click', (event) => {
  const link = patientNavigationLink(event)
  if (!link) return
  event.preventDefault()
  navigateToPatientView(link)
})

// Search waits for a quiet typing window before changing the canonical URL.
// The single page-stream effect aborts the prior search when this fires.
document.addEventListener('input', (event) => {
  if (!(event.target instanceof HTMLInputElement) || event.target.name !== 'q') return
  const form = event.target.closest(patientSearchSelector)
  if (form instanceof HTMLFormElement) schedulePatientSearch(form)
})

// Keep the GET form as a no-JavaScript fallback while making explicit submits
// immediate rather than subject to the typing debounce.
document.addEventListener('submit', (event) => {
  if (!(event.target instanceof HTMLFormElement) || !event.target.matches(patientSearchSelector)) return
  event.preventDefault()
  const timer = patientSearchTimers.get(event.target)
  if (timer) window.clearTimeout(timer)
  patientSearchTimers.delete(event.target)
  navigateToPatientSearch(event.target)
})
