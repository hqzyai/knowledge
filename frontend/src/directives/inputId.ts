import type { Directive } from 'vue'

// TDesign 1.x places unknown attributes on its wrapper; associate labels with the native input.
export const vInputId: Directive<HTMLElement, string> = (el, binding) => {
  const input = el.querySelector('input')
  if (input) input.id = binding.value
}
