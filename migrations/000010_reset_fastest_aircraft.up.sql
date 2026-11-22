-- K4NZA: the fastest list was built from unchecked speed readings (one bad
-- message could set a record, e.g. an A220 at 825 kt). Speeds are now only
-- recorded after they check out (core/speed_check.go), and the old entries
-- can't be checked after the fact, so the list starts over.
DELETE FROM fastest_aircraft;

-- indicated and true airspeed were saved in each other's columns
UPDATE slowest_aircraft
SET indicated_air_speed = true_air_speed,
    true_air_speed = indicated_air_speed;
