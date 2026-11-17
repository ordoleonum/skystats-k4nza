<script>
    import InterestingAircraft from "./InterestingAircraft.svelte";
    import { IconShield } from '@tabler/icons-svelte';
</script>

<InterestingAircraft
    endpoint='api/stats/interesting/police'
    title="Police Aircraft"
    icon={IconShield}
    group="Pol"
    aircraftType="pol"
    description="Law-enforcement aircraft, such as police helicopters and planes from city, state and federal agencies."
/>