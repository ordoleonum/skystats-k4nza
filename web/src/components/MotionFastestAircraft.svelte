<script>
    import MotionStats from './MotionStats.svelte';
    import { units } from '../stores/units';
    import { formatSpeed } from '../lib/units';
    import { IconRocket } from '@tabler/icons-svelte';

    // rebuilt when the units change
    $: columns = [
        { header: 'Reg', field: 'registration', class: 'font-mono whitespace-nowrap' },
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
            class: 'whitespace-nowrap',
            formatter: (value) => value ? new Date(value).toLocaleString() : '-'
        }
    ];
</script>

<MotionStats 
    endpoint="api/stats/motion/fastest"
    title="Fastest Aircraft"
    {columns}
    icon={IconRocket}
/>
