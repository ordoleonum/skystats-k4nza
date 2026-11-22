-- the removed entries can't be restored; swap the slowest airspeeds back
UPDATE slowest_aircraft
SET indicated_air_speed = true_air_speed,
    true_air_speed = indicated_air_speed;
