<script>
    import { stationTz } from '../lib/stationTz.js';
    import MotionStats from './MotionStats.svelte';
    import { units } from '../stores/units';
    import { formatAltitude } from '../lib/units';
    import { IconArrowDownDashed } from '@tabler/icons-svelte';

    // rebuilt when the units change
    $: columns = [
        { header: 'Reg', field: 'registration', class: 'font-mono' },
        { header: 'Model', field: 'type' },
        // { header: 'Flight', field: 'flight' },
        { 
            header: 'Altitude', 
            field: 'barometric_altitude',
            formatter: (value) => value ? formatAltitude(value, $units) : '-'
        },
        { 
            header: 'First Seen', 
            field: 'first_seen',
            formatter: (value) => value ? new Date(value).toLocaleString(undefined, { timeZone: stationTz() }) : '-'
        }
    ];
</script>

<MotionStats 
    endpoint="api/stats/motion/lowest"
    title="Lowest Aircraft"
    {columns}
    icon={IconArrowDownDashed}
/>
