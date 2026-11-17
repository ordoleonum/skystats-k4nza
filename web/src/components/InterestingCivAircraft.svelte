<script>
    import InterestingAircraft from "./InterestingAircraft.svelte";
    import { IconCurrencyDollar } from '@tabler/icons-svelte';
</script>

<InterestingAircraft
    endpoint='api/stats/interesting/civilian'
    title="Civilian Aircraft"
    icon={IconCurrencyDollar}
    group="Civ"
    aircraftType="civ"
    description="Notable civilian aircraft, such as private jets of celebrities and business leaders, sports teams, and historic or rare aircraft."
/>