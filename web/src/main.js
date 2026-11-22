import { mount } from 'svelte'
import '@fontsource-variable/plus-jakarta-sans'
import '@fontsource/ibm-plex-mono/500.css'
import '@fontsource/ibm-plex-mono/600.css'
import '../app.css'
import App from './App.svelte'
import { loadDefaultUnits } from './stores/units'
import { loadStationTz } from './lib/stationTz'

// use the feeder's default units until this browser picks its own
loadDefaultUnits()

// the station's time zone first, so every page asks for and shows the station's days
loadStationTz().then(() => {
  mount(App, {
    target: document.getElementById('app'),
  })
})
