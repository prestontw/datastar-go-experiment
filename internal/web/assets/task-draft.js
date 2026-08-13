// @ts-check

const taskFormSelector = 'form[data-task-draft]'
const taskDraftDelay = 500
let taskDraftTimer = 0

function cancelTaskDraftSave() {
  if (!taskDraftTimer) return
  window.clearTimeout(taskDraftTimer)
  taskDraftTimer = 0
}

function taskDraftForm() {
  const form = document.querySelector(taskFormSelector)
  return form instanceof HTMLFormElement ? form : null
}

function saveTaskDraft() {
  taskDraftForm()?.dispatchEvent(new CustomEvent('task-draft-save'))
}

export function flushTaskDraft() {
  if (!taskDraftTimer) return
  cancelTaskDraftSave()
  saveTaskDraft()
}

function scheduleTaskDraftSave() {
  cancelTaskDraftSave()
  taskDraftTimer = window.setTimeout(() => {
    taskDraftTimer = 0
    saveTaskDraft()
  }, taskDraftDelay)
}

document.addEventListener('input', (event) => {
  if (event.target instanceof Element && event.target.closest(taskFormSelector)) {
    scheduleTaskDraftSave()
  }
})

// Start pending work on pointer press even for links that intentionally retain
// native cross-page navigation and therefore bypass the patient controller.
document.addEventListener('mousedown', (event) => {
  if (event.button === 0 && event.target instanceof Element && event.target.closest('a[href]')) {
    flushTaskDraft()
  }
})

// A successful create command atomically clears the persisted draft. Cancel a
// pending local timer so it cannot enqueue redundant work after submission.
document.addEventListener('submit', (event) => {
  if (event.target instanceof HTMLFormElement && event.target.matches(taskFormSelector)) {
    cancelTaskDraftSave()
  }
})

// This remains best effort: starting the save when a tab becomes hidden is
// useful, but browsers may still terminate outstanding work during shutdown.
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') flushTaskDraft()
})
