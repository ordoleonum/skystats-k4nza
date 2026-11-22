-- K4NZA: record lists rebuilt.
--
-- Fastest: built from unchecked speed readings (one bad message could set a
-- record, e.g. an A220 at 825 kt). Speeds are now only recorded after they
-- check out (core/speed_check.go); old readings can't be checked after the
-- fact, so the Fastest list starts over with new flights.
DELETE FROM fastest_aircraft;

-- Highest, Lowest, Slowest: each visit was judged within 2 minutes of the
-- aircraft appearing and never again (Slowest showed planes taxiing before
-- take-off). They are rebuilt from the full visits already recorded.
DELETE FROM highest_aircraft;
DELETE FROM lowest_aircraft;
DELETE FROM slowest_aircraft;
UPDATE aircraft_data
SET highest_aircraft_processed = false,
    lowest_aircraft_processed = false,
    slowest_aircraft_processed = false
WHERE highest_aircraft_processed OR lowest_aircraft_processed OR slowest_aircraft_processed;

-- visits recorded before this update have unchecked speeds: keep them off the new Fastest list
UPDATE aircraft_data SET fastest_aircraft_processed = true WHERE fastest_aircraft_processed = false;
