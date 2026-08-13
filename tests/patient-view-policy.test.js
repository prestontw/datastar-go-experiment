// @ts-check

import assert from 'node:assert/strict'
import { test } from 'node:test'

import { patientSearchURL } from '../internal/web/assets/patient-view-policy.js'

const patientSearchTransitions = [
  {
    name: 'starting a search clears the current patient',
    state: { search: 'Noor', status: 'open', patientID: 'maya' },
    want: '/patients?q=Noor&status=open',
  },
  {
    name: 'refining a search clears a previously chosen result',
    state: { search: 'Noor Ahmed', status: 'done', patientID: 'noor' },
    want: '/patients?q=Noor+Ahmed&status=done',
  },
  {
    name: 'clearing search retains a chosen result',
    state: { search: '', status: 'open', patientID: 'noor' },
    want: '/patients?status=open&patient=noor',
  },
  {
    name: 'clearing search without a choice leaves selection absent',
    state: { search: '', status: 'all', patientID: '' },
    want: '/patients?status=all',
  },
  {
    name: 'values are trimmed and stale action parameters are replaced',
    state: { search: '  Maya  ', status: ' open ', patientID: ' elias ' },
    want: '/patients?q=Maya&status=open',
  },
]

test('patient search URL transition table', async (t) => {
  for (const transition of patientSearchTransitions) {
    await t.test(transition.name, () => {
      const url = patientSearchURL(
        'https://localhost/patients?patient=stale&q=stale',
        transition.state,
      )
      assert.equal(`${url.pathname}${url.search}`, transition.want)
    })
  }
})
