-- K4NZA: unidentified targets ("~" addresses: TIS-B relays, non-ICAO) no
-- longer appear on the record lists
DELETE FROM fastest_aircraft WHERE hex LIKE '~%';
DELETE FROM slowest_aircraft WHERE hex LIKE '~%';
DELETE FROM highest_aircraft WHERE hex LIKE '~%';
DELETE FROM lowest_aircraft WHERE hex LIKE '~%';
