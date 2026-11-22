<script>
    import { stationTz } from '../lib/stationTz.js';
    import MotionStats from './MotionStats.svelte';
    import { units } from '../stores/units';
    import { formatSpeed } from '../lib/units';
    import { IconWalk } from '@tabler/icons-svelte';

    // rebuilt when the units change
    $: columns = [
        { header: 'Reg', field: 'registration', class: 'font-mono' },
        { header: 'Type', field: 'type' },
        // { header: 'Flight', field: 'flight' },
        { 
            header: 'Speed', 
            field: 'ground_speed',
            formatter: (value) => value ? formatSpeed(value, $units) : '-'
        },
        { 
            header: 'First Seen', 
            field: 'first_seen',
            formatter: (value) => value ? new Date(value).toLocaleString(undefined, { timeZone: stationTz() }) : '-'
        }
    ];
</script>

<MotionStats 
    endpoint="api/stats/motion/slowest"
    title="Slowest Aircraft"
    {columns}
    icon={IconWalk}
/>
