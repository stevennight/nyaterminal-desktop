type EchoLine = {
  command: string
  pending: string
  reliable: boolean
  echoSafe: boolean
  echoJustConfirmed: boolean
}

type TerminalOutputToken =
  | { type: 'char'; value: string }
  | { type: 'newline' }

export class TerminalEchoGuard {
  private current: EchoLine = newEchoLine()
  private submitted: EchoLine[] = []

  get line() {
    return this.current.command
  }

  canSuggest() {
    return this.current.reliable && this.current.echoSafe && this.current.pending === ''
  }

  appendInput(value: string) {
    for (const char of Array.from(value)) {
      this.current.command += char
      this.current.echoJustConfirmed = false
      if (this.current.echoSafe) {
        this.current.pending += char
      }
    }
  }

  backspace() {
    if (!this.current.command) return
    this.current.command = dropLastCodePoint(this.current.command)
    this.current.echoJustConfirmed = false
    if (this.current.echoSafe && this.current.pending) {
      this.current.pending = dropLastCodePoint(this.current.pending)
    }
  }

  markUnreliable() {
    this.current.reliable = false
    this.markCurrentUnsafe()
  }

  submit() {
    const line = this.current
    this.current = newEchoLine()
    line.echoJustConfirmed = false
    if (!line.command.trim() || !line.reliable || !line.echoSafe) {
      return undefined
    }
    this.submitted.push(line)
    return undefined
  }

  observeOutput(text: string) {
    const confirmed: string[] = []
    for (const token of terminalOutputTokens(text)) {
      if (token.type === 'newline') {
        this.consumeLineBoundary(confirmed)
      } else {
        this.consumeVisibleChar(token.value)
      }
    }
    return confirmed
  }

  private consumeVisibleChar(char: string) {
    const submitted = this.submitted[0]
    if (submitted) {
      this.consumeSubmittedChar(submitted, char)
      return
    }
    this.consumeCurrentChar(char)
  }

  private consumeSubmittedChar(line: EchoLine, char: string) {
    if (!line.echoSafe) {
      this.submitted.shift()
      return
    }
    if (!line.pending) {
      this.submitted.shift()
      return
    }
    if (char !== firstCodePoint(line.pending)) {
      this.submitted.shift()
      return
    }
    line.pending = dropFirstCodePoint(line.pending)
  }

  private consumeCurrentChar(char: string) {
    if (!this.current.echoSafe) return
    if (!this.current.pending) {
      if (this.current.echoJustConfirmed) {
        this.markCurrentUnsafe()
      }
      return
    }
    if (char !== firstCodePoint(this.current.pending)) {
      this.markCurrentUnsafe()
      return
    }
    this.current.pending = dropFirstCodePoint(this.current.pending)
    this.current.echoJustConfirmed = this.current.pending === ''
  }

  private consumeLineBoundary(confirmed: string[]) {
    const submitted = this.submitted[0]
    if (submitted) {
      if (submitted.reliable && submitted.echoSafe && submitted.pending === '' && submitted.command.trim()) {
        confirmed.push(submitted.command)
      }
      this.submitted.shift()
      return
    }
    if (this.current.pending || this.current.echoJustConfirmed) {
      this.markCurrentUnsafe()
    }
  }

  private markCurrentUnsafe() {
    this.current.echoSafe = false
    this.current.pending = ''
    this.current.echoJustConfirmed = false
  }
}

function newEchoLine(): EchoLine {
  return {
    command: '',
    pending: '',
    reliable: true,
    echoSafe: true,
    echoJustConfirmed: false,
  }
}

function terminalOutputTokens(text: string) {
  const tokens: TerminalOutputToken[] = []
  for (let index = 0; index < text.length;) {
    const char = text[index]
    if (char === '\x1b') {
      index = skipEscapeSequence(text, index)
      continue
    }
    if (char === '\r' || char === '\n') {
      tokens.push({ type: 'newline' })
      index += char === '\r' && text[index + 1] === '\n' ? 2 : 1
      continue
    }
    if (char === '\b') {
      index += text[index + 1] === ' ' && text[index + 2] === '\b' ? 3 : 1
      continue
    }
    if (char === '\x7f' || char < ' ') {
      index++
      continue
    }
    const codePoint = text.codePointAt(index)
    if (codePoint === undefined) break
    const value = String.fromCodePoint(codePoint)
    tokens.push({ type: 'char', value })
    index += value.length
  }
  return tokens
}

function skipEscapeSequence(text: string, start: number) {
  let index = start + 1
  const introducer = text[index]
  if (!introducer) return index
  index++
  if (introducer === '[') {
    while (index < text.length) {
      const code = text.charCodeAt(index++)
      if (code >= 0x40 && code <= 0x7e) break
    }
    return index
  }
  if (introducer === ']' || introducer === 'P' || introducer === '^' || introducer === '_') {
    while (index < text.length) {
      if (text[index] === '\x07') return index + 1
      if (text[index] === '\x1b' && text[index + 1] === '\\') return index + 2
      index++
    }
    return index
  }
  return index
}

function firstCodePoint(value: string) {
  return Array.from(value)[0] ?? ''
}

function dropFirstCodePoint(value: string) {
  const [, ...rest] = Array.from(value)
  return rest.join('')
}

function dropLastCodePoint(value: string) {
  const chars = Array.from(value)
  chars.pop()
  return chars.join('')
}
