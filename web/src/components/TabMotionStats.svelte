<script>
    import MotionFastestAircraft from './MotionFastestAircraft.svelte';
    import MotionSlowestAircraft from './MotionSlowestAircraft.svelte';
    import MotionHighestAircraft from './MotionHighestAircraft.svelte';
    import MotionLowestAircraft from './MotionLowestAircraft.svelte';
    import NotableEvents from './NotableEvents.svelte';
</script>

<!-- the notable events log first, then the record lists -->
<NotableEvents />

<div class="grid grid-cols-1 lg:grid-cols-2 mt-6 gap-6">
    <MotionFastestAircraft />
    <MotionSlowestAircraft />
    <MotionHighestAircraft /> 
    <MotionLowestAircraft />
</div>