// @ts-check

const patientNavigationSelector = 'a[data-patient-navigation]'

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

/** @param {HTMLAnchorElement} link */
function navigateToPatientView(link) {
  const url = new URL(link.href, document.baseURI)
  const destination = `${url.pathname}${url.search}${url.hash}`
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`
  if (destination === current) return

  window.dispatchEvent(
    new CustomEvent('patient-navigation', { detail: destination }),
  )
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
