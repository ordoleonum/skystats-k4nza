import { mount } from 'svelte'
import '../app.css'
import App from './App.svelte'
import { loadDefaultUnits } from './stores/units'

// use the feeder's default units until this browser picks its own
loadDefaultUnits()

const app = mount(App, {
  target: document.getElementById('app'),
})

export default app
