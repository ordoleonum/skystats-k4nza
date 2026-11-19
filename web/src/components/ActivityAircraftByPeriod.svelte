<script>
    import FlightsByPeriod from './charts/FlightsByPeriod.svelte';
    let selectedTab = 0;
</script>

<div class="bg-base-100 rounded-box shadow-md pt-6 p-8">
  <div>
      <div class="flex flex-wrap justify-between items-center gap-2 mb-6">
      <h2 class="text-2xl font-extralight tracking-wider ">Aircraft Seen</h2>
      <div class="tabs tabs-box flex-nowrap">
        <input
          type="radio"
          name="activity_tabs_aircraft"
          class="tab"
          aria-label="24hrs"
          bind:group={selectedTab}
          value={0}
          checked
        />
        <input
          type="radio"
          name="activity_tabs_aircraft"
          class="tab"
          aria-label="Month"
          bind:group={selectedTab}
          value={1}
        />
        <input
          type="radio"
          name="activity_tabs_aircraft"
          class="tab"
          aria-label="Year"
          bind:group={selectedTab}
          value={2}
        />
    </div>
    </div>
      {#if selectedTab === 0}
          <FlightsByPeriod type="aircraft" period="day"/>
      {:else if selectedTab === 1}
          <FlightsByPeriod type="aircraft" period="month"/>
      {:else if selectedTab === 2}
          <FlightsByPeriod type="aircraft" period="year"/>
      {/if}
      <p class="text-xs opacity-60 mt-3">Different aircraft heard in each hour, day or month; a sudden drop that isn't a quiet time of day can mean a reception problem.</p>
  </div>


</div>
