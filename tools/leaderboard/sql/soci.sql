SELECT
  u.id AS id,
  u.name AS name,
  CAST(r.position AS INTEGER) AS position,
  CAST(r.score AS INTEGER) AS score,
  CAST(r.stamps_points AS INTEGER) AS stamps_points,
  CAST(r.medal_points AS INTEGER) AS medal_points,
  CAST(r.events_points AS INTEGER) AS events_points,
  CAST(r.stamps_total AS INTEGER) AS stamps_total,
  CAST(r.stamps_distinct_regions AS INTEGER) AS stamps_distinct_regions
FROM users u
JOIN (
  -- Le parti sono arrotondate prima di sommarle e ordinarle: cosi' a pari
  -- punti mostrati la posizione e' la stessa e le tre parti sommano al totale.
  SELECT s.*, s.stamps_points + s.events_points + s.medal_points AS score,
         RANK() OVER (ORDER BY s.stamps_points + s.events_points + s.medal_points DESC) AS position
  FROM (
    SELECT
      m.id AS uid,
      ROUND(COALESCE(d.points, 0)) AS stamps_points,
      m.stamps_total, m.stamps_distinct_regions,
      -- Medaglie, come in MedalRules dell'app: bronzo 3, argento 5, oro 10,
      -- platino 20, quelle a livello unico 8. Fuori: "organizer" e
      -- "national_director" (gli eventi organizzati hanno gia' i loro punti
      -- piu' sotto) e "traveller" (stamps_out_of_region e' rotto:
      -- members_registry.state e' la provincia).
        CASE WHEN m.stamps_total >= 250 THEN 20 WHEN m.stamps_total >= 100 THEN 10 WHEN m.stamps_total >= 25 THEN 5 WHEN m.stamps_total >= 1 THEN 3 ELSE 0 END
      + CASE WHEN m.stamps_distinct_months >= 24 THEN 20 WHEN m.stamps_distinct_months >= 12 THEN 10 WHEN m.stamps_distinct_months >= 6 THEN 5 WHEN m.stamps_distinct_months >= 3 THEN 3 ELSE 0 END
      + CASE WHEN m.stamps_max_month_streak >= 24 THEN 20 WHEN m.stamps_max_month_streak >= 12 THEN 10 WHEN m.stamps_max_month_streak >= 6 THEN 5 WHEN m.stamps_max_month_streak >= 3 THEN 3 ELSE 0 END
      + CASE WHEN m.stamps_max_same_day >= 8 THEN 20 WHEN m.stamps_max_same_day >= 5 THEN 10 WHEN m.stamps_max_same_day >= 3 THEN 5 WHEN m.stamps_max_same_day >= 2 THEN 3 ELSE 0 END
      + CASE WHEN m.stamps_max_seasons_in_year >= 4 THEN 8 ELSE 0 END
      + CASE WHEN m.stamps_pioneer >= 30 THEN 20 WHEN m.stamps_pioneer >= 15 THEN 10 WHEN m.stamps_pioneer >= 5 THEN 5 WHEN m.stamps_pioneer >= 1 THEN 3 ELSE 0 END
      + CASE WHEN m.stamps_distinct_regions >= 20 THEN 20 WHEN m.stamps_distinct_regions >= 10 THEN 10 WHEN m.stamps_distinct_regions >= 5 THEN 5 WHEN m.stamps_distinct_regions >= 2 THEN 3 ELSE 0 END
      + CASE WHEN m.stamps_national >= 10 THEN 20 WHEN m.stamps_national >= 5 THEN 10 WHEN m.stamps_national >= 3 THEN 5 WHEN m.stamps_national >= 1 THEN 3 ELSE 0 END
      + CASE WHEN m.stamps_night >= 1 THEN 8 ELSE 0 END
      + CASE WHEN m.stamps_morning >= 1 THEN 8 ELSE 0 END
      + CASE WHEN m.stamps_halloween >= 1 THEN 8 ELSE 0 END
      + CASE WHEN m.stamps_new_year >= 1 THEN 8 ELSE 0 END
      + CASE WHEN m.stamps_valentine >= 1 THEN 8 ELSE 0 END
      + CASE WHEN m.stamps_pi_day >= 1 THEN 8 ELSE 0 END
      + CASE WHEN m.stamps_birthday >= 1 THEN 8 ELSE 0 END
      + CASE WHEN m.stamps_first_day >= 1 THEN 8 ELSE 0 END
      + CASE WHEN m.stamps_late >= 1 THEN 8 ELSE 0 END
      + CASE WHEN m.event_registrations >= 50 THEN 20 WHEN m.event_registrations >= 25 THEN 10 WHEN m.event_registrations >= 10 THEN 5 WHEN m.event_registrations >= 1 THEN 3 ELSE 0 END
      + CASE WHEN m.is_membership_active THEN 8 ELSE 0 END
      + CASE WHEN (julianday('now') - julianday(m.app_since)) / 365 >= 5 THEN 20 WHEN (julianday('now') - julianday(m.app_since)) / 365 >= 3 THEN 10 WHEN (julianday('now') - julianday(m.app_since)) / 365 >= 2 THEN 5 WHEN (julianday('now') - julianday(m.app_since)) / 365 >= 1 THEN 3 ELSE 0 END
      + CASE WHEN m.calendar_linked THEN 8 ELSE 0 END
      + CASE WHEN m.calendar_regions >= 10 THEN 20 WHEN m.calendar_regions >= 5 THEN 10 WHEN m.calendar_regions >= 3 THEN 5 WHEN m.calendar_regions >= 1 THEN 3 ELSE 0 END
      + CASE WHEN m.devices >= 2 THEN 8 ELSE 0 END
      AS medal_points,
      ROUND(COALESCE(ev.points, 0)) AS events_points
    FROM view_medals m
    JOIN users u2 ON u2.id = m.id
    LEFT JOIN (
      -- Timbri: fino a 20 punti ciascuno, secondo la quota di soci della
      -- regione dell'evento che l'ha preso (organizzatore escluso):
      -- s = timbratori / (N_r^0,7 * Nmedia^0,3), con N_r i soci attivi della
      -- regione e Nmedia quelli di una regione media. L'esponente 0,7
      -- perche' l'affluenza non cresce in proporzione ai soci: il
      -- denominatore cresce meno che linearmente con la regione. Sotto il 2%
      -- niente, dal 15% tutto, in mezzo una S (smoothstep t^2 (3 - 2t), con
      -- t = (s - 2%) / 13%) che vale meta' all'8,5%. Le soglie sono quote
      -- dei soci della regione, quindi lo stesso numero di timbratori pesa
      -- diversamente in regioni di dimensione diversa. Scende a zero in 12
      -- mesi (24 se l'evento e' nazionale).
      SELECT user, SUM(20.0 * decay * t * t * (3 - 2 * t) * community) AS points
      FROM (
        SELECT c.user, c.decay,
          MIN(1.0, MAX(0.0, (cnt.claimers / (POWER(COALESCE(reg.active, nb.avg_active), 0.7) * POWER(nb.avg_active, 0.3)) - 0.02) / 0.13)) AS t,
          -- comunita' della regione (view_leaderboard_regioni): quanto e'
          -- ampia, rispetto ai soci, la comunita' che timbra ai suoi eventi.
          -- Eventi nazionali e senza regione: 1.
          COALESCE(rc.community_pct, 100) / 100.0 AS community
        FROM (
          SELECT su.id, su.user, su.stamp, su.created, e.position, e.owner, COALESCE(e.is_national, 0) AS is_national,
            MAX(0, 1 - (julianday('now') - julianday(su.created))
                   / CASE WHEN e.is_national THEN 730 ELSE 365 END) AS decay,
            -- ogni timbro va all'edizione con quel nome piu' vicina nel tempo
            ROW_NUMBER() OVER (PARTITION BY su.id
                               ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
          FROM stamp_users su
          JOIN stamp st ON st.id = su.stamp
          LEFT JOIN events e ON e.name = st.description
        ) c
        JOIN (
          -- soci che hanno preso quel timbro, esclusi chi ne ha organizzato
          -- l'edizione e 5366, l'account che prende ogni timbro
          SELECT stamp, COUNT(*) AS claimers
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
        ) cnt ON cnt.stamp = c.stamp
        LEFT JOIN positions p ON p.id = c.position
        LEFT JOIN (
          -- Soci attivi per regione, dal registro soci (members_registry
          -- .is_active e .area), app o non app, con gli stessi accorpamenti
          -- degli eventi.
          SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
                 COUNT(*) * 1.0 AS active
          FROM members_registry mr
          WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
          GROUP BY 1
        ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT c.is_national
        CROSS JOIN (
          -- Soci di una regione media: entrano nel denominatore con
          -- esponente 0,3, per gli eventi nazionali e senza regione fanno
          -- anche da N_r.
          SELECT AVG(active) AS avg_active FROM (
            SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
            WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
            GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
        ) nb
        LEFT JOIN view_leaderboard_regioni rc ON rc.region_key = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT c.is_national
        -- chi ha organizzato quell'edizione non prende punti dal suo timbro,
        -- le edizioni di altri, prima o dopo la sua, contano come a tutti
        WHERE c.nearest = 1 AND COALESCE(c.owner, '') != c.user
          -- lo staff regionale (local_offices_admins, segretari e collaboratori)
          -- non prende punti dai timbri degli eventi della propria regione,
          -- finche' e' in carica, gli eventi che organizza gli danno E. I
          -- nomi delle sedi non coincidono con positions.state: mappati qui.
          AND NOT (NOT c.is_national AND EXISTS (
            SELECT 1 FROM local_offices_admins la JOIN local_offices lo ON lo.id = la.local_office
            WHERE la.user = c.user
              AND CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
                  = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END))
      )
      GROUP BY user
    ) d ON d.user = m.id
    LEFT JOIN (
      -- Eventi organizzati: fino a 10 punti ciascuno, con la stessa S sulla
      -- quota di soci della regione timbratori e la stessa scadenza, 12 mesi
      -- dalla data dell'evento, 24 se nazionale.
      SELECT owner, SUM(10.0 * decay * t * t * (3 - 2 * t) * community) AS points
      FROM (
        SELECT e.owner,
          MAX(0, 1 - MAX(0, julianday('now') - julianday(e.when_start))
                 / CASE WHEN e.is_national THEN 730 ELSE 365 END) AS decay,
          MIN(1.0, MAX(0.0, (cnt.claimers / (POWER(COALESCE(reg.active, nb.avg_active), 0.7) * POWER(nb.avg_active, 0.3)) - 0.02) / 0.13)) AS t,
          -- comunita' della regione, come per i timbri
          COALESCE(rc.community_pct, 100) / 100.0 AS community
        FROM events e
        JOIN (
          -- ogni timbro preso va all'evento con quel nome piu' vicino nel
          -- tempo, esclusi l'organizzatore e 5366, che prende ogni timbro
          SELECT event_id, COUNT(*) AS claimers
          FROM (
            SELECT e.id AS event_id, e.owner, su.user,
              ROW_NUMBER() OVER (PARTITION BY su.id
                                 ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
            FROM stamp_users su
            JOIN stamp st ON st.id = su.stamp
            JOIN events e ON e.name = st.description
            WHERE su.user != '' AND su.user != '5366'
          )
          WHERE nearest = 1 AND owner != user
          GROUP BY event_id
        ) cnt ON cnt.event_id = e.id
        LEFT JOIN positions p ON p.id = e.position
        LEFT JOIN (
          -- Soci attivi per regione, dal registro soci (members_registry
          -- .is_active e .area), app o non app, con gli stessi accorpamenti
          -- degli eventi.
          SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
                 COUNT(*) * 1.0 AS active
          FROM members_registry mr
          WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
          GROUP BY 1
        ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT e.is_national
        CROSS JOIN (
          -- Soci di una regione media: entrano nel denominatore con
          -- esponente 0,3, per gli eventi nazionali e senza regione fanno
          -- anche da N_r.
          SELECT AVG(active) AS avg_active FROM (
            SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
            WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
            GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
        ) nb
        LEFT JOIN view_leaderboard_regioni rc ON rc.region_key = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AND NOT e.is_national
        WHERE e.when_start != ''
      )
      GROUP BY owner
    ) ev ON ev.owner = m.id
    WHERE m.is_membership_active
      AND m.id != '5366'
      AND COALESCE(u2.access_level, '') != 'blocked'
      AND EXISTS (SELECT 1 FROM stamp_users su WHERE su.user = m.id)
      -- fuori chi ha scelto di non apparire (users_metadata, chiave
      -- leaderboard_hidden = true, impostata dall'app): la riga non viene
      -- calcolata, gli altri salgono di posizione
      AND NOT EXISTS (SELECT 1 FROM users_metadata um
                      WHERE um.user = m.id AND um.key = 'leaderboard_hidden' AND um.value = 'true')
  ) s
) r ON r.uid = u.id
