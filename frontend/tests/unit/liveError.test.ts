// studio-ui "Live controls": the server's reasons for refusing a go live or a destination change, as the studio
// shows them (translated when known, the server's own message otherwise).
import { afterEach, describe, expect, it } from 'vitest'
import { ApiError } from '@/api/client'
import { locale } from '@/i18n'
import { liveError } from '@/stores/studio'

const conflict = (m: string) => new ApiError(409, 'conflict', m)

describe('liveError', () => {
  afterEach(() => {
    locale.value = 'en'
  })

  it.each([
    ['Choose at least one destination before going live.', 'Escolha ao menos um destino antes de entrar ao vivo.'],
    ['The server is at its limit of 2 live broadcasts.', 'O servidor está no limite de 2 transmissões ao vivo.'],
    [
      'The destination "YouTube" is in use by another broadcast.',
      'O destino “YouTube” está em uso por outra transmissão.',
    ],
    [
      'A live broadcast needs at least one destination; end the broadcast to remove the last one.',
      'Uma transmissão ao vivo precisa de ao menos um destino; encerre a transmissão para remover o último.',
    ],
  ])('translates %j', (server, pt) => {
    locale.value = 'pt-BR'
    expect(liveError(conflict(server))).toBe(pt)
  })

  it('keeps the reason in English', () => {
    expect(liveError(conflict('The destination "Twitch" is in use by another broadcast.'))).toBe(
      'The destination “Twitch” is in use by another broadcast.',
    )
  })

  it('shows an unknown reason as the server wrote it, and other errors by their code', () => {
    expect(liveError(conflict('Something new.'))).toBe('Something new.')
    expect(liveError(new ApiError(502, 'bad_gateway', 'x'))).toBe('An external service did not answer correctly.')
  })
})
