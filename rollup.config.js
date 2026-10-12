import deckyPlugin from '@decky/rollup'

const config = deckyPlugin({})
// @decky/rollup hardcodes source maps and ignores overrides passed in, so the release zip would
// ship dist/index.js.map.
config.output.sourcemap = false

export default config
