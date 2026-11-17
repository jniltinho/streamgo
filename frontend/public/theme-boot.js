// Portions adapted from MeetHub (MIT): frontend/public/theme-boot.js
// Applies the theme before the first paint (external file: no inline script needed).
// Precedence (ui-themes spec): the signed-in user's saved theme (rendered by the server into data-user-theme), else
// the theme remembered in this browser, else streamgo-dark, or streamgo-light when the browser prefers a light scheme.
;(function () {
  var root = document.documentElement
  var theme = root.getAttribute('data-user-theme')
  if (!theme || theme.charAt(0) === '%') {
    try {
      theme = localStorage.getItem('streamgo.theme')
    } catch {
      theme = null
    }
  }
  if (!theme) {
    var light = window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches
    theme = light ? 'streamgo-light' : 'streamgo-dark'
  }
  root.setAttribute('data-theme', theme)
})()
