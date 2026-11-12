<script>
    import InterestingAircraft from "./InterestingAircraft.svelte";
    import { IconBuildings } from '@tabler/icons-svelte';
</script>

<InterestingAircraft
    endpoint='api/stats/interesting/government'
    title="Government Aircraft"
    icon={IconBuildings}
    aircraftType="gov"
    description="Aircraft flown for national or local governments, such as VIP and head-of-state transport, customs and border patrol, firefighting, and survey planes."
/>
    