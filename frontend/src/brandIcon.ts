export const BRAND_ICON_SRC = '/nyaterminal-icon-dark-gray.svg'
export const BRAND_ICON_LIGHT_SRC = '/nyaterminal-icon-light.svg'

export function brandIconSrc(theme: string) {
  return theme === 'light' ? BRAND_ICON_LIGHT_SRC : BRAND_ICON_SRC
}
