<script>
    import AircraftByPeriod from './charts/AircraftByPeriod.svelte';
    export let aircraftorflight = 'flights';
    let selectedTab = 0;
    let title = aircraftorflight === 'flights' ? 'Aircraft Type (flight count)' : 'Aircraft Type (unique aircraft)';
</script>

<div class="bg-base-100 rounded-box shadow-md pt-6 p-8">
    <div>
      <div class="flex flex-wrap justify-between items-center gap-2 mb-6">
      <h2 class="text-2xl font-extralight tracking-wider ">{title}</h2>
      <div class="tabs tabs-box flex-nowrap">
        <input
          type="radio"
          name="activity_tabs_{aircraftorflight}_types"
          class="tab"
          aria-label="24hrs"
          bind:group={selectedTab}
          value={0}
          checked
        />
        <input
          type="radio"
          name="activity_tabs_{aircraftorflight}_types"
          class="tab"
          aria-label="Month"
          bind:group={selectedTab}
          value={1}
        />
        <input
          type="radio"
          name="activity_tabs_{aircraftorflight}_types"
          class="tab"
          aria-label="Year"
          bind:group={selectedTab}
          value={2}
        />
        <input
          type="radio"
          name="activity_tabs"
          class="tab"
          aria-label="All"
          bind:group={selectedTab}
          value={3}
        />
    </div>
    </div>
      {#if selectedTab === 0}
          <AircraftByPeriod period="day" type={aircraftorflight}/>
      {:else if selectedTab === 1}
          <AircraftByPeriod period="month" type={aircraftorflight}/>
      {:else if selectedTab === 2}
          <AircraftByPeriod period="year" type={aircraftorflight}/>
      {:else if selectedTab === 3}
          <AircraftByPeriod period="all" type={aircraftorflight}/>
      {/if}
      <p class="text-xs opacity-60 mt-3">{aircraftorflight === 'flights' ? 'The aircraft types you hear most, counted once per flight.' : 'The aircraft types you hear most, each aircraft counted once however often it flies over.'}</p>
  </div>
</div>


