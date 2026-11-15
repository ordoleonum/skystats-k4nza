<script>
  import ActivityAircraftByPeriod from "./ActivityAircraftByPeriod.svelte";
  import MetricAircraftSeen from "./MetricAircraftSeen.svelte";
  import MetricFlightsSeen from "./MetricFlightsSeen.svelte";
  import ActivityTopTypesByPeriod from './ActivityTopTypesByPeriod.svelte';
  import ActivityRangeByDirection from './ActivityRangeByDirection.svelte';
  import ActivitySignalVsDistance from './ActivitySignalVsDistance.svelte';
</script>

<div class="grid grid-cols-1 lg:grid-cols-1 xl:grid-cols-2 mt-10 gap-6">
  <div class="col">
    <MetricFlightsSeen />
  </div>
  <div class="col">
    <MetricAircraftSeen />
  </div>
  <div class="col">
    <ActivityRangeByDirection />
  </div>
  <div class="col">
    <ActivitySignalVsDistance />
  </div>
  <div class="col">
    <ActivityAircraftByPeriod />
  </div>
  <div class="col">
    <ActivityTopTypesByPeriod aircraftorflight="aircraft"/>
  </div>
</div>
