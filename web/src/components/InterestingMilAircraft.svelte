<script>
    import InterestingAircraft from "./InterestingAircraft.svelte";
    import { IconTank } from '@tabler/icons-svelte';
</script>

<InterestingAircraft
    endpoint='api/stats/interesting/military'
    title="Military Aircraft"
    icon={IconTank}
    group="Mil"
    aircraftType="mil"
    description="Air force, army and navy aircraft, such as tankers, transports, fighters, trainers, patrol and surveillance planes, and military helicopters."
/>