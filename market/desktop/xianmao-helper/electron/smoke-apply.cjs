/** one-shot: electron electron/smoke-apply.cjs */
const { app } = require('electron')

app.whenReady().then(async () => {
  try {
    require('level')
    console.log('LEVEL_OK')
  } catch (e) {
    console.log('LEVEL_FAIL', e && e.message)
  }
  try {
    const { applyClientConfig } = require('./apply-config.cjs')
    const r = await applyClientConfig(
      {
        base_url: 'http://127.0.0.1:8080',
        key: 'tm-u5vPozH72f0aerOnwTXqa2ps8H2YVsqTySy0I7rd',
        key_id: 6,
        provider: '闲猫',
      },
      'cherry-studio',
    )
    console.log('APPLY', JSON.stringify(r))
  } catch (e) {
    console.log('APPLY_FAIL', e && (e.stack || e.message))
  }
  app.exit(0)
})
