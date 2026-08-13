// @ts-check

import { patientSearchURL } from './patient-view-policy.js'
import { flushTaskDraft } from './task-draft.js'

const patientNavigationSelector = 'a[data-patient-navigation]'
const patientSearchSelector = 'form[data-patient-search]'
const patientSearchDelay = 300
let patientSearchTimer = 0

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

function cancelPatientSearch() {
  if (!patientSearchTimer) return
  window.clearTimeout(patientSearchTimer)
  patientSearchTimer = 0
}

/** @param {HTMLAnchorElement} link */
function navigateToPatientView(link) {
  // Flush the old patient's draft before changing task context. A debounce
  // scheduled from the previous patient list must never overwrite the newer
  // choice after the pointer is pressed.
  flushTaskDraft()
  cancelPatientSearch()
  dispatchPatientURL('patient-navigation', new URL(link.href, document.baseURI))
}

/**
 * @param {FormData} data
 * @param {string} name
 */
function stringFormValue(data, name) {
  const value = data.get(name)
  return typeof value === 'string' ? value : ''
}

/** @param {HTMLFormElement} form */
function navigateToPatientSearch(form) {
  flushTaskDraft()
  const data = new FormData(form)
  const url = patientSearchURL(form.action, {
    search: stringFormValue(data, 'q'),
    status: stringFormValue(data, 'status'),
    patientID: stringFormValue(data, 'patient'),
  })
  dispatchPatientURL('patient-search', url)
}

/** @param {HTMLFormElement} form */
function schedulePatientSearch(form) {
  cancelPatientSearch()
  patientSearchTimer = window.setTimeout(() => {
    patientSearchTimer = 0
    navigateToPatientSearch(form)
  }, patientSearchDelay)
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
  cancelPatientSearch()
  navigateToPatientSearch(event.target)
})

// Capture runs before Datastar's bubbling popstate handler clears task signals.
window.addEventListener('popstate', flushTaskDraft, { capture: true })
