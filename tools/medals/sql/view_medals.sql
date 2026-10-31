-- Contatori del medagliere, una riga per socio. Due famiglie (vedi
-- tools/medals/rules.go):
--   * stagionali: timbri, eventi organizzati, iscrizioni agli eventi e
--     biglietti, limitati alla stagione dei parametri start e end (istanti
--     UTC, vedi tools/season): i timbri del socio con created nella
--     finestra, gli eventi con when_start, iscrizioni e biglietti con
--     created. Il filtro su quali timbri valgono (organizzatori, staff
--     regionale, quota di soci timbratori) resta a vita: guarda tutti i
--     timbri di quell'edizione, non solo quelli della stagione. Il Pioniere
--     usa la graduatoria a vita e conta solo i timbri della stagione;
--   * permanenti: app_since, expire_membership, is_membership_active,
--     renewed_ahead, devices, calendar_linked, calendar_regions, payments,
--     donations, boutique_orders, sempre a vita.
SELECT
  u.id AS id,

  u.created AS app_since,
  u.expire_membership AS expire_membership,
  u.is_membership_active AS is_membership_active,
  CAST(u.expire_membership > DATETIME('now', '+365 days') AS BOOL) AS renewed_ahead,

  CAST(COALESCE(s.stamps_total, 0) AS INTEGER) AS stamps_total,
  s.first_stamp_at AS first_stamp_at,
  CAST(COALESCE(s.stamps_months, 0) AS INTEGER) AS stamps_distinct_months,
  CAST(COALESCE(s.stamps_night, 0) AS INTEGER) AS stamps_night,
  CAST(COALESCE(s.stamps_morning, 0) AS INTEGER) AS stamps_morning,
  CAST(COALESCE(s.stamps_halloween, 0) AS INTEGER) AS stamps_halloween,
  CAST(COALESCE(s.stamps_new_year, 0) AS INTEGER) AS stamps_new_year,
  CAST(COALESCE(s.stamps_valentine, 0) AS INTEGER) AS stamps_valentine,
  CAST(COALESCE(s.stamps_pi_day, 0) AS INTEGER) AS stamps_pi_day,
  CAST(COALESCE(s.stamps_birthday, 0) AS INTEGER) AS stamps_birthday,
  CAST(COALESCE(s.stamps_first_day, 0) AS INTEGER) AS stamps_first_day,
  CAST(COALESCE(s.stamps_late, 0) AS INTEGER) AS stamps_late,

  CAST(COALESCE(d.max_same_day, 0) AS INTEGER) AS stamps_max_same_day,
  CAST(COALESCE(ms.max_month_streak, 0) AS INTEGER) AS stamps_max_month_streak,
  CAST(COALESCE(se.max_seasons_in_year, 0) AS INTEGER) AS stamps_max_seasons_in_year,

  CAST(COALESCE(pi.stamps_pioneer, 0) AS INTEGER) AS stamps_pioneer,

  CAST(COALESCE(g.stamp_regions, 0) AS INTEGER) AS stamps_distinct_regions,
  CAST(COALESCE(g.stamps_national, 0) AS INTEGER) AS stamps_national,
  CAST(COALESCE(g.stamps_out_of_region, 0) AS INTEGER) AS stamps_out_of_region,

  CAST((SELECT COUNT(*) FROM tickets t WHERE t.user_id = u.id
          AND t.created >= {:start} AND t.created < {:end}) AS INTEGER) AS tickets_total,
  CAST((SELECT COUNT(*) FROM events e WHERE e.owner = u.id
          AND e.when_start >= {:start} AND e.when_start < {:end}) AS INTEGER) AS events_created,
  CAST((SELECT COUNT(*) FROM events e WHERE e.owner = u.id AND e.is_national
          AND e.when_start >= {:start} AND e.when_start < {:end}) AS INTEGER) AS events_created_national,
  CAST((SELECT COUNT(*) FROM events_schedule_subscribers es WHERE es.user = u.id
          AND es.created >= {:start} AND es.created < {:end}) AS INTEGER) AS event_registrations,
  CAST((SELECT COUNT(*) FROM payments pa WHERE pa.user = u.id AND pa.status = 'succeeded') AS INTEGER) AS payments_succeeded,
  CAST((SELECT COUNT(*) FROM payments pa WHERE pa.user = u.id AND pa.status = 'succeeded'
          AND LOWER(pa.description) LIKE '%donazion%') AS INTEGER) AS donations,
  CAST((SELECT COUNT(*) FROM boutique_orders bo WHERE bo.user = u.id) AS INTEGER) AS boutique_orders,
  CAST((SELECT COUNT(*) FROM users_devices ud WHERE ud.user = u.id) AS INTEGER) AS devices,
  CAST((SELECT COUNT(*) FROM calendar_link cl WHERE cl.user = u.id) > 0 AS BOOL) AS calendar_linked,
  CAST(COALESCE((SELECT MAX(JSON_ARRAY_LENGTH(cl.state)) FROM calendar_link cl
          WHERE cl.user = u.id AND JSON_VALID(cl.state)), 0) AS INTEGER) AS calendar_regions

FROM users u

LEFT JOIN (
  SELECT
    x.user,
    COUNT(*) AS stamps_total,
    MIN(x.created) AS first_stamp_at,
    COUNT(DISTINCT STRFTIME('%Y-%m', x.lt)) AS stamps_months,
    SUM(CAST(STRFTIME('%H', x.lt) AS INTEGER) < 4) AS stamps_night,
    SUM(CAST(STRFTIME('%H', x.lt) AS INTEGER) BETWEEN 5 AND 8) AS stamps_morning,
    SUM(STRFTIME('%m-%d', x.lt) = '10-31') AS stamps_halloween,
    SUM(STRFTIME('%m-%d', x.lt) IN ('12-31', '01-01')) AS stamps_new_year,
    SUM(STRFTIME('%m-%d', x.lt) = '02-14') AS stamps_valentine,
    SUM(STRFTIME('%m-%d', x.lt) = '03-14') AS stamps_pi_day,
    SUM(mr.birthdate IS NOT NULL AND mr.birthdate != ''
        AND STRFTIME('%m-%d', x.lt) = STRFTIME('%m-%d', DATETIME(mr.birthdate, '+2 hours'))) AS stamps_birthday,
    SUM(JULIANDAY(x.created) - JULIANDAY(x.stamp_created) <= 1) AS stamps_first_day,
    SUM(JULIANDAY(x.created) - JULIANDAY(x.stamp_created) >= 182) AS stamps_late
  FROM (
    SELECT su.user, su.created, DATETIME(su.created, '+2 hours') AS lt, st.created AS stamp_created
    FROM (SELECT su0.id, su0.stamp, su0.user, su0.created FROM stamp_users su0
    -- fuori il timbro di chi ha organizzato l'edizione piu' vicina nel tempo
    -- al timbro, le edizioni di altri, prima o dopo la sua, contano
    WHERE su0.created >= {:start} AND su0.created < {:end}
      AND NOT EXISTS (SELECT 1 FROM stamp st0 JOIN events e0 ON e0.name = st0.description WHERE st0.id = su0.stamp AND e0.owner = su0.user
                      AND NOT EXISTS (SELECT 1 FROM events e1 WHERE e1.name = st0.description AND e1.owner != su0.user
                                      AND abs(julianday(su0.created) - julianday(e1.when_start)) < abs(julianday(su0.created) - julianday(e0.when_start))))
      -- lo staff regionale (local_offices_admins) non conta i timbri degli
      -- eventi non nazionali della propria regione, finche' e' in carica
      AND NOT EXISTS (SELECT 1 FROM stamp sx JOIN events er ON er.name = sx.description JOIN positions pr ON pr.id = er.position
                      JOIN local_offices_admins la ON la.user = su0.user JOIN local_offices lo ON lo.id = la.local_office
                      WHERE sx.id = su0.stamp AND NOT er.is_national
                        AND CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
                            = CASE pr.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE pr.state END)
      -- conta solo i timbri che in classifica valgono almeno un punto a
      -- tempo zero (view_leaderboard, senza scadenza ne' comunita'):
      -- 20 * sigma(s) >= 1, con s = timbratori / (N_r^0,7 * Nmedia^0,3),
      -- timbratori esclusi l'organizzatore dell'edizione e 5366, N_r i soci
      -- attivi della regione dell'edizione piu' vicina alla creazione del
      -- timbro (Nmedia se nazionale o senza regione). La soglia e' una
      -- quota dei soci, quindi lo stesso numero di timbratori basta in una
      -- regione piccola e non in una grande.
      AND su0.stamp IN (
        SELECT k.stamp FROM (
          SELECT cs.stamp,
            MIN(1.0, MAX(0.0, (cs.claimers / (POWER(COALESCE(reg.active, nb.avg_active), 0.7) * POWER(nb.avg_active, 0.3)) - 0.02) / 0.13)) AS t
          FROM (
            SELECT stamp, COUNT(*) * 1.0 AS claimers
            FROM (
              SELECT su.stamp, su.user, e.owner,
                ROW_NUMBER() OVER (PARTITION BY su.id
                                   ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
              FROM stamp_users su
              JOIN stamp st ON st.id = su.stamp
              LEFT JOIN events e ON e.name = st.description
              WHERE su.user != '' AND su.user != '5366'
            )
            WHERE nearest = 1 AND COALESCE(owner, '') != user
            GROUP BY stamp
          ) cs
          LEFT JOIN (
            SELECT stamp, position, is_national FROM (
              SELECT st.id AS stamp, e.position, COALESCE(e.is_national, 0) AS is_national,
                ROW_NUMBER() OVER (PARTITION BY st.id
                                   ORDER BY abs(julianday(st.created) - julianday(e.when_start))) AS nearest
              FROM stamp st
              JOIN events e ON e.name = st.description
            )
            WHERE nearest = 1
          ) ne ON ne.stamp = cs.stamp
          LEFT JOIN positions p ON p.id = ne.position
          LEFT JOIN (
            SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
                   COUNT(*) * 1.0 AS active
            FROM members_registry mr
            WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
            GROUP BY 1
          ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT COALESCE(ne.is_national, 0)
          CROSS JOIN (
            SELECT AVG(active) AS avg_active FROM (
              SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
              WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
              GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
          ) nb
        ) k
        WHERE 20.0 * k.t * k.t * (3 - 2 * k.t) >= 1)) su
    JOIN stamp st ON st.id = su.stamp
  ) x
  LEFT JOIN members_registry mr ON mr.id = x.user
  GROUP BY x.user
) s ON s.user = u.id

LEFT JOIN (
  SELECT user, MAX(n) AS max_same_day
  FROM (
    SELECT user, DATE(DATETIME(created, '+2 hours')) AS day, COUNT(*) AS n
    FROM (SELECT su0.id, su0.stamp, su0.user, su0.created FROM stamp_users su0
    -- fuori il timbro di chi ha organizzato l'edizione piu' vicina nel tempo
    -- al timbro, le edizioni di altri, prima o dopo la sua, contano
    WHERE su0.created >= {:start} AND su0.created < {:end}
      AND NOT EXISTS (SELECT 1 FROM stamp st0 JOIN events e0 ON e0.name = st0.description WHERE st0.id = su0.stamp AND e0.owner = su0.user
                      AND NOT EXISTS (SELECT 1 FROM events e1 WHERE e1.name = st0.description AND e1.owner != su0.user
                                      AND abs(julianday(su0.created) - julianday(e1.when_start)) < abs(julianday(su0.created) - julianday(e0.when_start))))
      -- lo staff regionale (local_offices_admins) non conta i timbri degli
      -- eventi non nazionali della propria regione, finche' e' in carica
      AND NOT EXISTS (SELECT 1 FROM stamp sx JOIN events er ON er.name = sx.description JOIN positions pr ON pr.id = er.position
                      JOIN local_offices_admins la ON la.user = su0.user JOIN local_offices lo ON lo.id = la.local_office
                      WHERE sx.id = su0.stamp AND NOT er.is_national
                        AND CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
                            = CASE pr.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE pr.state END)
      -- conta solo i timbri che in classifica valgono almeno un punto a
      -- tempo zero (view_leaderboard, senza scadenza ne' comunita'):
      -- 20 * sigma(s) >= 1, con s = timbratori / (N_r^0,7 * Nmedia^0,3),
      -- timbratori esclusi l'organizzatore dell'edizione e 5366, N_r i soci
      -- attivi della regione dell'edizione piu' vicina alla creazione del
      -- timbro (Nmedia se nazionale o senza regione). La soglia e' una
      -- quota dei soci, quindi lo stesso numero di timbratori basta in una
      -- regione piccola e non in una grande.
      AND su0.stamp IN (
        SELECT k.stamp FROM (
          SELECT cs.stamp,
            MIN(1.0, MAX(0.0, (cs.claimers / (POWER(COALESCE(reg.active, nb.avg_active), 0.7) * POWER(nb.avg_active, 0.3)) - 0.02) / 0.13)) AS t
          FROM (
            SELECT stamp, COUNT(*) * 1.0 AS claimers
            FROM (
              SELECT su.stamp, su.user, e.owner,
                ROW_NUMBER() OVER (PARTITION BY su.id
                                   ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
              FROM stamp_users su
              JOIN stamp st ON st.id = su.stamp
              LEFT JOIN events e ON e.name = st.description
              WHERE su.user != '' AND su.user != '5366'
            )
            WHERE nearest = 1 AND COALESCE(owner, '') != user
            GROUP BY stamp
          ) cs
          LEFT JOIN (
            SELECT stamp, position, is_national FROM (
              SELECT st.id AS stamp, e.position, COALESCE(e.is_national, 0) AS is_national,
                ROW_NUMBER() OVER (PARTITION BY st.id
                                   ORDER BY abs(julianday(st.created) - julianday(e.when_start))) AS nearest
              FROM stamp st
              JOIN events e ON e.name = st.description
            )
            WHERE nearest = 1
          ) ne ON ne.stamp = cs.stamp
          LEFT JOIN positions p ON p.id = ne.position
          LEFT JOIN (
            SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
                   COUNT(*) * 1.0 AS active
            FROM members_registry mr
            WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
            GROUP BY 1
          ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT COALESCE(ne.is_national, 0)
          CROSS JOIN (
            SELECT AVG(active) AS avg_active FROM (
              SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
              WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
              GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
          ) nb
        ) k
        WHERE 20.0 * k.t * k.t * (3 - 2 * k.t) >= 1))
    GROUP BY user, day
  )
  GROUP BY user
) d ON d.user = u.id

LEFT JOIN (
  SELECT user, MAX(len) AS max_month_streak
  FROM (
    SELECT user, COUNT(*) AS len
    FROM (
      SELECT user, m, m - ROW_NUMBER() OVER (PARTITION BY user ORDER BY m) AS grp
      FROM (
        SELECT DISTINCT
          user,
          CAST(STRFTIME('%Y', DATETIME(created, '+2 hours')) AS INTEGER) * 12
            + CAST(STRFTIME('%m', DATETIME(created, '+2 hours')) AS INTEGER) AS m
        FROM (SELECT su0.id, su0.stamp, su0.user, su0.created FROM stamp_users su0
    -- fuori il timbro di chi ha organizzato l'edizione piu' vicina nel tempo
    -- al timbro, le edizioni di altri, prima o dopo la sua, contano
    WHERE su0.created >= {:start} AND su0.created < {:end}
      AND NOT EXISTS (SELECT 1 FROM stamp st0 JOIN events e0 ON e0.name = st0.description WHERE st0.id = su0.stamp AND e0.owner = su0.user
                      AND NOT EXISTS (SELECT 1 FROM events e1 WHERE e1.name = st0.description AND e1.owner != su0.user
                                      AND abs(julianday(su0.created) - julianday(e1.when_start)) < abs(julianday(su0.created) - julianday(e0.when_start))))
      -- lo staff regionale (local_offices_admins) non conta i timbri degli
      -- eventi non nazionali della propria regione, finche' e' in carica
      AND NOT EXISTS (SELECT 1 FROM stamp sx JOIN events er ON er.name = sx.description JOIN positions pr ON pr.id = er.position
                      JOIN local_offices_admins la ON la.user = su0.user JOIN local_offices lo ON lo.id = la.local_office
                      WHERE sx.id = su0.stamp AND NOT er.is_national
                        AND CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
                            = CASE pr.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE pr.state END)
      -- conta solo i timbri che in classifica valgono almeno un punto a
      -- tempo zero (view_leaderboard, senza scadenza ne' comunita'):
      -- 20 * sigma(s) >= 1, con s = timbratori / (N_r^0,7 * Nmedia^0,3),
      -- timbratori esclusi l'organizzatore dell'edizione e 5366, N_r i soci
      -- attivi della regione dell'edizione piu' vicina alla creazione del
      -- timbro (Nmedia se nazionale o senza regione). La soglia e' una
      -- quota dei soci, quindi lo stesso numero di timbratori basta in una
      -- regione piccola e non in una grande.
      AND su0.stamp IN (
        SELECT k.stamp FROM (
          SELECT cs.stamp,
            MIN(1.0, MAX(0.0, (cs.claimers / (POWER(COALESCE(reg.active, nb.avg_active), 0.7) * POWER(nb.avg_active, 0.3)) - 0.02) / 0.13)) AS t
          FROM (
            SELECT stamp, COUNT(*) * 1.0 AS claimers
            FROM (
              SELECT su.stamp, su.user, e.owner,
                ROW_NUMBER() OVER (PARTITION BY su.id
                                   ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
              FROM stamp_users su
              JOIN stamp st ON st.id = su.stamp
              LEFT JOIN events e ON e.name = st.description
              WHERE su.user != '' AND su.user != '5366'
            )
            WHERE nearest = 1 AND COALESCE(owner, '') != user
            GROUP BY stamp
          ) cs
          LEFT JOIN (
            SELECT stamp, position, is_national FROM (
              SELECT st.id AS stamp, e.position, COALESCE(e.is_national, 0) AS is_national,
                ROW_NUMBER() OVER (PARTITION BY st.id
                                   ORDER BY abs(julianday(st.created) - julianday(e.when_start))) AS nearest
              FROM stamp st
              JOIN events e ON e.name = st.description
            )
            WHERE nearest = 1
          ) ne ON ne.stamp = cs.stamp
          LEFT JOIN positions p ON p.id = ne.position
          LEFT JOIN (
            SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
                   COUNT(*) * 1.0 AS active
            FROM members_registry mr
            WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
            GROUP BY 1
          ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT COALESCE(ne.is_national, 0)
          CROSS JOIN (
            SELECT AVG(active) AS avg_active FROM (
              SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
              WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
              GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
          ) nb
        ) k
        WHERE 20.0 * k.t * k.t * (3 - 2 * k.t) >= 1))
      )
    )
    GROUP BY user, grp
  )
  GROUP BY user
) ms ON ms.user = u.id

LEFT JOIN (
  SELECT user, MAX(n) AS max_seasons_in_year
  FROM (
    SELECT user, y, COUNT(DISTINCT season) AS n
    FROM (
      SELECT
        user,
        -- l'anno della stagione (da ottobre si conta sull'anno dopo), cosi'
        -- autunno, inverno, primavera ed estate dello stesso campionato
        -- stanno insieme e le quattro stagioni si possono fare in una
        CAST(STRFTIME('%Y', DATETIME(created, '+2 hours')) AS INTEGER)
          + (CAST(STRFTIME('%m', DATETIME(created, '+2 hours')) AS INTEGER) >= 10) AS y,
        CASE CAST(STRFTIME('%m', DATETIME(created, '+2 hours')) AS INTEGER)
          WHEN 12 THEN 1 WHEN 1 THEN 1 WHEN 2 THEN 1
          WHEN 3 THEN 2 WHEN 4 THEN 2 WHEN 5 THEN 2
          WHEN 6 THEN 3 WHEN 7 THEN 3 WHEN 8 THEN 3
          ELSE 4
        END AS season
      FROM (SELECT su0.id, su0.stamp, su0.user, su0.created FROM stamp_users su0
    -- fuori il timbro di chi ha organizzato l'edizione piu' vicina nel tempo
    -- al timbro, le edizioni di altri, prima o dopo la sua, contano
    WHERE su0.created >= {:start} AND su0.created < {:end}
      AND NOT EXISTS (SELECT 1 FROM stamp st0 JOIN events e0 ON e0.name = st0.description WHERE st0.id = su0.stamp AND e0.owner = su0.user
                      AND NOT EXISTS (SELECT 1 FROM events e1 WHERE e1.name = st0.description AND e1.owner != su0.user
                                      AND abs(julianday(su0.created) - julianday(e1.when_start)) < abs(julianday(su0.created) - julianday(e0.when_start))))
      -- lo staff regionale (local_offices_admins) non conta i timbri degli
      -- eventi non nazionali della propria regione, finche' e' in carica
      AND NOT EXISTS (SELECT 1 FROM stamp sx JOIN events er ON er.name = sx.description JOIN positions pr ON pr.id = er.position
                      JOIN local_offices_admins la ON la.user = su0.user JOIN local_offices lo ON lo.id = la.local_office
                      WHERE sx.id = su0.stamp AND NOT er.is_national
                        AND CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
                            = CASE pr.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE pr.state END)
      -- conta solo i timbri che in classifica valgono almeno un punto a
      -- tempo zero (view_leaderboard, senza scadenza ne' comunita'):
      -- 20 * sigma(s) >= 1, con s = timbratori / (N_r^0,7 * Nmedia^0,3),
      -- timbratori esclusi l'organizzatore dell'edizione e 5366, N_r i soci
      -- attivi della regione dell'edizione piu' vicina alla creazione del
      -- timbro (Nmedia se nazionale o senza regione). La soglia e' una
      -- quota dei soci, quindi lo stesso numero di timbratori basta in una
      -- regione piccola e non in una grande.
      AND su0.stamp IN (
        SELECT k.stamp FROM (
          SELECT cs.stamp,
            MIN(1.0, MAX(0.0, (cs.claimers / (POWER(COALESCE(reg.active, nb.avg_active), 0.7) * POWER(nb.avg_active, 0.3)) - 0.02) / 0.13)) AS t
          FROM (
            SELECT stamp, COUNT(*) * 1.0 AS claimers
            FROM (
              SELECT su.stamp, su.user, e.owner,
                ROW_NUMBER() OVER (PARTITION BY su.id
                                   ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
              FROM stamp_users su
              JOIN stamp st ON st.id = su.stamp
              LEFT JOIN events e ON e.name = st.description
              WHERE su.user != '' AND su.user != '5366'
            )
            WHERE nearest = 1 AND COALESCE(owner, '') != user
            GROUP BY stamp
          ) cs
          LEFT JOIN (
            SELECT stamp, position, is_national FROM (
              SELECT st.id AS stamp, e.position, COALESCE(e.is_national, 0) AS is_national,
                ROW_NUMBER() OVER (PARTITION BY st.id
                                   ORDER BY abs(julianday(st.created) - julianday(e.when_start))) AS nearest
              FROM stamp st
              JOIN events e ON e.name = st.description
            )
            WHERE nearest = 1
          ) ne ON ne.stamp = cs.stamp
          LEFT JOIN positions p ON p.id = ne.position
          LEFT JOIN (
            SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
                   COUNT(*) * 1.0 AS active
            FROM members_registry mr
            WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
            GROUP BY 1
          ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT COALESCE(ne.is_national, 0)
          CROSS JOIN (
            SELECT AVG(active) AS avg_active FROM (
              SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
              WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
              GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
          ) nb
        ) k
        WHERE 20.0 * k.t * k.t * (3 - 2 * k.t) >= 1))
    )
    GROUP BY user, y
  )
  GROUP BY user
) se ON se.user = u.id

LEFT JOIN (
  -- Pioniere: tra i primi dieci a prendere quel timbro (graduatoria su tutti
  -- i timbri validi, a vita), contano quelli presi dal socio nella stagione.
  SELECT user, SUM(rk <= 10 AND created >= {:start} AND created < {:end}) AS stamps_pioneer
  FROM (
    SELECT user, created, ROW_NUMBER() OVER (PARTITION BY stamp ORDER BY created) AS rk
    FROM (SELECT su0.id, su0.stamp, su0.user, su0.created FROM stamp_users su0
    -- fuori il timbro di chi ha organizzato l'edizione piu' vicina nel tempo
    -- al timbro, le edizioni di altri, prima o dopo la sua, contano
    WHERE NOT EXISTS (SELECT 1 FROM stamp st0 JOIN events e0 ON e0.name = st0.description WHERE st0.id = su0.stamp AND e0.owner = su0.user
                      AND NOT EXISTS (SELECT 1 FROM events e1 WHERE e1.name = st0.description AND e1.owner != su0.user
                                      AND abs(julianday(su0.created) - julianday(e1.when_start)) < abs(julianday(su0.created) - julianday(e0.when_start))))
      -- lo staff regionale (local_offices_admins) non conta i timbri degli
      -- eventi non nazionali della propria regione, finche' e' in carica
      AND NOT EXISTS (SELECT 1 FROM stamp sx JOIN events er ON er.name = sx.description JOIN positions pr ON pr.id = er.position
                      JOIN local_offices_admins la ON la.user = su0.user JOIN local_offices lo ON lo.id = la.local_office
                      WHERE sx.id = su0.stamp AND NOT er.is_national
                        AND CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
                            = CASE pr.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE pr.state END)
      -- conta solo i timbri che in classifica valgono almeno un punto a
      -- tempo zero (view_leaderboard, senza scadenza ne' comunita'):
      -- 20 * sigma(s) >= 1, con s = timbratori / (N_r^0,7 * Nmedia^0,3),
      -- timbratori esclusi l'organizzatore dell'edizione e 5366, N_r i soci
      -- attivi della regione dell'edizione piu' vicina alla creazione del
      -- timbro (Nmedia se nazionale o senza regione). La soglia e' una
      -- quota dei soci, quindi lo stesso numero di timbratori basta in una
      -- regione piccola e non in una grande.
      AND su0.stamp IN (
        SELECT k.stamp FROM (
          SELECT cs.stamp,
            MIN(1.0, MAX(0.0, (cs.claimers / (POWER(COALESCE(reg.active, nb.avg_active), 0.7) * POWER(nb.avg_active, 0.3)) - 0.02) / 0.13)) AS t
          FROM (
            SELECT stamp, COUNT(*) * 1.0 AS claimers
            FROM (
              SELECT su.stamp, su.user, e.owner,
                ROW_NUMBER() OVER (PARTITION BY su.id
                                   ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
              FROM stamp_users su
              JOIN stamp st ON st.id = su.stamp
              LEFT JOIN events e ON e.name = st.description
              WHERE su.user != '' AND su.user != '5366'
            )
            WHERE nearest = 1 AND COALESCE(owner, '') != user
            GROUP BY stamp
          ) cs
          LEFT JOIN (
            SELECT stamp, position, is_national FROM (
              SELECT st.id AS stamp, e.position, COALESCE(e.is_national, 0) AS is_national,
                ROW_NUMBER() OVER (PARTITION BY st.id
                                   ORDER BY abs(julianday(st.created) - julianday(e.when_start))) AS nearest
              FROM stamp st
              JOIN events e ON e.name = st.description
            )
            WHERE nearest = 1
          ) ne ON ne.stamp = cs.stamp
          LEFT JOIN positions p ON p.id = ne.position
          LEFT JOIN (
            SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
                   COUNT(*) * 1.0 AS active
            FROM members_registry mr
            WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
            GROUP BY 1
          ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT COALESCE(ne.is_national, 0)
          CROSS JOIN (
            SELECT AVG(active) AS avg_active FROM (
              SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
              WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
              GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
          ) nb
        ) k
        WHERE 20.0 * k.t * k.t * (3 - 2 * k.t) >= 1))
  )
  GROUP BY user
) pi ON pi.user = u.id

LEFT JOIN (
  SELECT
    su.user,
    COUNT(DISTINCT CASE WHEN p.state NOT IN ('', 'NaN') THEN p.state END) AS stamp_regions,
    COUNT(DISTINCT CASE WHEN e.is_national THEN su.stamp END) AS stamps_national,
    COUNT(DISTINCT CASE WHEN p.state NOT IN ('', 'NaN') AND mr.state != '' AND p.state != mr.state
                        THEN su.stamp END) AS stamps_out_of_region
  FROM (SELECT su0.id, su0.stamp, su0.user, su0.created FROM stamp_users su0
    -- fuori il timbro di chi ha organizzato l'edizione piu' vicina nel tempo
    -- al timbro, le edizioni di altri, prima o dopo la sua, contano
    WHERE su0.created >= {:start} AND su0.created < {:end}
      AND NOT EXISTS (SELECT 1 FROM stamp st0 JOIN events e0 ON e0.name = st0.description WHERE st0.id = su0.stamp AND e0.owner = su0.user
                      AND NOT EXISTS (SELECT 1 FROM events e1 WHERE e1.name = st0.description AND e1.owner != su0.user
                                      AND abs(julianday(su0.created) - julianday(e1.when_start)) < abs(julianday(su0.created) - julianday(e0.when_start))))
      -- lo staff regionale (local_offices_admins) non conta i timbri degli
      -- eventi non nazionali della propria regione, finche' e' in carica
      AND NOT EXISTS (SELECT 1 FROM stamp sx JOIN events er ON er.name = sx.description JOIN positions pr ON pr.id = er.position
                      JOIN local_offices_admins la ON la.user = su0.user JOIN local_offices lo ON lo.id = la.local_office
                      WHERE sx.id = su0.stamp AND NOT er.is_national
                        AND CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
                            = CASE pr.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE pr.state END)
      -- conta solo i timbri che in classifica valgono almeno un punto a
      -- tempo zero (view_leaderboard, senza scadenza ne' comunita'):
      -- 20 * sigma(s) >= 1, con s = timbratori / (N_r^0,7 * Nmedia^0,3),
      -- timbratori esclusi l'organizzatore dell'edizione e 5366, N_r i soci
      -- attivi della regione dell'edizione piu' vicina alla creazione del
      -- timbro (Nmedia se nazionale o senza regione). La soglia e' una
      -- quota dei soci, quindi lo stesso numero di timbratori basta in una
      -- regione piccola e non in una grande.
      AND su0.stamp IN (
        SELECT k.stamp FROM (
          SELECT cs.stamp,
            MIN(1.0, MAX(0.0, (cs.claimers / (POWER(COALESCE(reg.active, nb.avg_active), 0.7) * POWER(nb.avg_active, 0.3)) - 0.02) / 0.13)) AS t
          FROM (
            SELECT stamp, COUNT(*) * 1.0 AS claimers
            FROM (
              SELECT su.stamp, su.user, e.owner,
                ROW_NUMBER() OVER (PARTITION BY su.id
                                   ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
              FROM stamp_users su
              JOIN stamp st ON st.id = su.stamp
              LEFT JOIN events e ON e.name = st.description
              WHERE su.user != '' AND su.user != '5366'
            )
            WHERE nearest = 1 AND COALESCE(owner, '') != user
            GROUP BY stamp
          ) cs
          LEFT JOIN (
            SELECT stamp, position, is_national FROM (
              SELECT st.id AS stamp, e.position, COALESCE(e.is_national, 0) AS is_national,
                ROW_NUMBER() OVER (PARTITION BY st.id
                                   ORDER BY abs(julianday(st.created) - julianday(e.when_start))) AS nearest
              FROM stamp st
              JOIN events e ON e.name = st.description
            )
            WHERE nearest = 1
          ) ne ON ne.stamp = cs.stamp
          LEFT JOIN positions p ON p.id = ne.position
          LEFT JOIN (
            SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
                   COUNT(*) * 1.0 AS active
            FROM members_registry mr
            WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
            GROUP BY 1
          ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT COALESCE(ne.is_national, 0)
          CROSS JOIN (
            SELECT AVG(active) AS avg_active FROM (
              SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
              WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
              GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
          ) nb
        ) k
        WHERE 20.0 * k.t * k.t * (3 - 2 * k.t) >= 1)) su
  JOIN stamp st ON st.id = su.stamp
  JOIN events e ON e.name = st.description
  LEFT JOIN positions p ON p.id = e.position
  LEFT JOIN members_registry mr ON mr.id = su.user
  GROUP BY su.user
) g ON g.user = u.id
