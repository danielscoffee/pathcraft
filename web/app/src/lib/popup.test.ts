import { describe, expect, it } from 'vitest'
import { popupContent } from './popup'

class TestNode {
  children: TestNode[] = []
  textContent: string | null = null

  readonly tagName: string

  constructor(tagName: string) {
    this.tagName = tagName
  }

  append(...nodes: TestNode[]) {
    this.children.push(...nodes)
  }
}

const testDocument = {
  createElement: (tagName: string) => new TestNode(tagName),
  createTextNode: (text: string) => {
    const node = new TestNode('#text')
    node.textContent = text
    return node
  },
}

describe('popupContent', () => {
  it('keeps hostile feed values as text', () => {
    const hostile = '<img src=x onerror=globalThis.pwned=true>'
    const popup = popupContent(hostile, [hostile], testDocument as unknown as Document) as unknown as TestNode

    expect(popup.children.some((node) => node.tagName === 'img')).toBe(false)
    expect(popup.children[0].textContent).toBe(hostile)
    expect(popup.children.at(-1)?.textContent).toBe(hostile)
  })
})
