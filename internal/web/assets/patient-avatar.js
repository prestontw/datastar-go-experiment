// @ts-check

/**
 * A tiny standards-only Web Component used by both server-rendered pages.
 * Its content and Shadow DOM styles are intentionally colocated here.
 */
class PatientAvatar extends HTMLElement {
  /** @type {HTMLSpanElement} */
  #label

  static get observedAttributes() {
    return ['initials', 'tone', 'size']
  }

  constructor() {
    super()
    const root = this.attachShadow({ mode: 'open' })
    const style = document.createElement('style')
    style.textContent = `
      :host {
        --avatar-color: #235c50;
        --avatar-background: #d8eee8;
        display: inline-grid;
        width: 2.55rem;
        height: 2.55rem;
        flex: 0 0 auto;
        place-items: center;
        border: 1px solid color-mix(in srgb, var(--avatar-color), transparent 76%);
        border-radius: .78rem;
        color: var(--avatar-color);
        background: var(--avatar-background);
        font: 800 .72rem/1 ui-sans-serif, system-ui, sans-serif;
        letter-spacing: .04em;
      }
      :host([size='large']) { width: 3.2rem; height: 3.2rem; border-radius: 1rem; font-size: .88rem; }
      :host([tone='clay']) { --avatar-color: #864735; --avatar-background: #f6dfd6; }
      :host([tone='gold']) { --avatar-color: #755717; --avatar-background: #f5e8bd; }
      :host([tone='sky']) { --avatar-color: #315c74; --avatar-background: #dceaf1; }
      span { transform: translateY(.02em); }
    `
    this.#label = document.createElement('span')
    this.#label.setAttribute('part', 'initials')
    root.append(style, this.#label)
  }

  connectedCallback() {
    this.#render()
  }

  attributeChangedCallback() {
    this.#render()
  }

  #render() {
    this.#label.textContent = this.getAttribute('initials')?.slice(0, 3) || '?'
  }
}

if (!customElements.get('patient-avatar')) {
  customElements.define('patient-avatar', PatientAvatar)
}
