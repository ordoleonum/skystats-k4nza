import { mount } from 'svelte'
// first: sends data requests to the chosen receiver (Radar Nodes)
import './lib/receiver.js'
import '@fontsource-variable/plus-jakarta-sans'
import '@fontsource/ibm-plex-mono/500.css'
import '@fontsource/ibm-plex-mono/600.css'
import '../app.css'
import App from './App.svelte'
import { loadDefaultUnits } from './stores/units'

// use the feeder's default units until this browser picks its own
loadDefaultUnits()

const app = mount(App, {
  target: document.getElementById('app'),
})

export default app
