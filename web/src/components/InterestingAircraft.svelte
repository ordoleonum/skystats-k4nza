<script>
    import { onMount } from 'svelte'
    import { fmtStationDay, fmtStationTime } from '../lib/stationTz.js';
    import { settings, refreshInterestingData } from '../stores/settings';
    import { searchInNewTab, searchGroup } from '../lib/search';

    export let endpoint;
    export let title;
    export let icon;
    export let aircraftType;
    export let description = '';
    // plane-alert-db group (Mil, Gov, Pol, Civ): the header opens Search with all of them
    export let group = '';

    let data = [];
    let loading = true;
    let error = null;
    let selectedAircraft = null;
    let imageLoadingStates = {
        image1: true,
        image2: true,
        image3: true
    };

    // "Today 7:24 PM", "Yesterday 6:39 PM", "Nov 16 2:25 PM" - as in Search
    function fmtSeen(v) {
        if (!v) return '–';
        // on the station's calendar and clock
        const d = new Date(v);
        if (isNaN(d)) return '–';
        return `${fmtStationDay(d)} ${fmtStationTime(d)}`;
    }

    async function fetchData() {
        
        try {
            const response = await fetch(endpoint);
            if(!response.ok) {
                throw new Error(`{response.status}`);
            }
            const result = await response.json();
            data = result;
            error = null
        } catch (err) {
            error = err.message;
        } finally {
            loading = false;
        }
    }

    function showAircraftModal(aircraft) {
        selectedAircraft = aircraft;
        imageLoadingStates = {
            image1: true,
            image2: true,
            image3: true
        };
        // @ts-ignore
        document.getElementById(aircraftType).showModal();
    }

    function closeModal() {
        selectedAircraft = null;
    }

    onMount(() => {
        fetchData();
    })

    // Refresh when settings change
    $: if ($refreshInterestingData) {
        fetchData();
    }

    $: disableTags = $settings['disable_planealertdb_tags']?.setting_value === 'true';

</script>

<div>
    <div class="card bg-base-100 mb-4 w96 shadow-sm rounded hover:shadow-md transition-all duration-200">
        <div class="card-body">
            <div class="overflow-x-auto">
                <!-- header: always shown, so empty categories still say what they are -->
                <div class="flex items-center gap-2 {description ? 'mb-1' : 'mb-5'}">
                {#if icon}
                    <div class="w-8 h-8 rounded-lg flex items-center justify-center">
                        <svelte:component this={icon} class="w-6 h-6 {group ? `grp-text-${group}` : 'text-primary'}" />
                    </div>
                {/if}
                {#if group}
                    <h2 class="text-2xl font-extralight tracking-wider">
                        <button type="button" class="link link-hover text-left" title="Search your history for every aircraft in this group" on:click={() => searchGroup(group)}>{title}</button>
                    </h2>
                {:else}
                    <h2 class="text-2xl font-extralight tracking-wider">{title}</h2>
                {/if}
                </div>
                {#if description}
                    <p class="text-sm opacity-70 mb-5">{description}</p>
                {/if}
                {#if loading}
                    <div class="flex justify-center py-8">
                        <span class="loading loading-ring loading-lg"></span>
                    </div>
                {:else if error}
                    <div class="flex alert alert-error">
                        <svg xmlns="http://www.w3.org/2000/svg" class="stroke-current shrink-0 h-6 w-6" fill="none" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
                        <span>Something went wrong: {error}</span>
                    </div>
                {:else if data.length === 0}
                    <div class="rounded-box border border-dashed border-base-content/20 px-4 py-6 text-center text-sm opacity-70">
                        None seen yet. They'll appear here as soon as one flies within range.
                    </div>
                {:else}
                    <!-- table-->
                    <table class="table">
                        <thead class="uppercase tracking-wider">
                            <tr>
                                <th>Reg</th>
                                <th>Operator</th>
                                <th>Type</th>
                                <th>Last Seen</th>
                            </tr>
                        </thead>
                        <tbody>
                            {#each data as aircraft}
                            <tr class="hover:bg-base-300 cursor-pointer" on:click={() => showAircraftModal(aircraft)}>
                                <td class="font-mono whitespace-nowrap">
                                    {#if aircraft.registration}
                                        <button type="button" class="link link-hover text-primary font-mono" title="Search your history for {aircraft.registration} (opens a new tab)" on:click|stopPropagation={() => searchInNewTab(aircraft.registration)}>{aircraft.registration}</button>
                                    {:else}-{/if}
                                </td>
                                <td>{aircraft.operator || '–'}</td>
                                <td>{aircraft.type || aircraft.icao_type || '–'}</td>
                                <td class="whitespace-nowrap">{fmtSeen(aircraft.seen)}</td>
                            </tr>
                            {/each}
                        </tbody>
                    </table>
                {/if}
            </div>
        </div>
    </div>
</div>

<!--modal-->
<dialog id={aircraftType} class="modal" on:close={closeModal}>
    <div class="modal-box max-w-4xl">
        {#if selectedAircraft}
            <div class="flex items-center justify-between mb-2">
                <h3 class="text-lg font-bold">{selectedAircraft.registration || selectedAircraft.icao}{selectedAircraft.type ? ` - ${selectedAircraft.type}` : ""}</h3>
                {#if disableTags === false}
                    <div class="flex gap-2">
                        {#if selectedAircraft.tag1}
                            <div class="badge badge-accent text-white">{selectedAircraft.tag1}</div>
                        {/if}
                        {#if selectedAircraft.tag2}
                            <div class="badge badge-accent text-white">{selectedAircraft.tag2}</div>
                        {/if}
                        {#if selectedAircraft.tag3}
                            <div class="badge badge-accent text-white">{selectedAircraft.tag3}</div>
                        {/if}
                    </div>
                {/if}
            </div>
            <p class="text-sm text-gray-600 mb-4">{selectedAircraft.operator} {#if selectedAircraft.flight} - {selectedAircraft.flight} {/if}</p>
            <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {#if selectedAircraft.image_link_1}
                    <div class="relative">
                        {#if imageLoadingStates.image1}
                            <div class="skeleton h-48 w-full rounded-lg"></div>
                        {/if}
                        <img 
                            src={selectedAircraft.image_link_1} 
                            alt="{selectedAircraft.registration} photo 1" 
                            class="w-full h-auto rounded-lg {imageLoadingStates.image1 ? 'absolute inset-0 opacity-0' : ''}"
                            on:load={() => imageLoadingStates.image1 = false}
                            on:error={() => imageLoadingStates.image1 = false}
                        />
                    </div>
                {/if}
                {#if selectedAircraft.image_link_2}
                    <div class="relative">
                        {#if imageLoadingStates.image2}
                            <div class="skeleton h-48 w-full rounded-lg"></div>
                        {/if}
                        <img 
                            src={selectedAircraft.image_link_2} 
                            alt="{selectedAircraft.registration} photo 2" 
                            class="w-full h-auto rounded-lg {imageLoadingStates.image2 ? 'absolute inset-0 opacity-0' : ''}"
                            on:load={() => imageLoadingStates.image2 = false}
                            on:error={() => imageLoadingStates.image2 = false}
                        />
                    </div>
                {/if}
                {#if selectedAircraft.image_link_3}
                    <div class="relative">
                        {#if imageLoadingStates.image3}
                            <div class="skeleton h-48 w-full rounded-lg"></div>
                        {/if}
                        <img 
                            src={selectedAircraft.image_link_3} 
                            alt="{selectedAircraft.registration} photo 3" 
                            class="w-full h-auto rounded-lg {imageLoadingStates.image3 ? 'absolute inset-0 opacity-0' : ''}"
                            on:load={() => imageLoadingStates.image3 = false}
                            on:error={() => imageLoadingStates.image3 = false}
                        />
                    </div>
                {/if}
            </div>
            {#if !selectedAircraft.image_link_1 && !selectedAircraft.image_link_2 && !selectedAircraft.image_link_3}
                <p class="text-center text-gray-500 py-8">No photos available for this aircraft</p>
            {/if}
        {/if}
        <div class="modal-action">
            <form method="dialog">
                <button class="btn">Close</button>
            </form>
        </div>
    </div>
    <form method="dialog" class="modal-backdrop">
        <button>close</button>
    </form>
</dialog>
