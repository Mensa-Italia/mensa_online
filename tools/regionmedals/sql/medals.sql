-- Medaglie di regione, una riga per sede (local_offices), per l'anno dato
-- dal parametro year. Definizioni e soglie in medals.go: qui solo i
-- numeri grezzi, il metallo lo decide il codice con le soglie, cosi' le
-- soglie stanno in un posto solo. Il Podio non e' qui: lo decide lo
-- snapshot di fine anno (leaderboard_history), qui c'e' solo la posizione
-- di adesso in view_leaderboard_regioni.
--
-- Socio "visto": ogni timbro preso va all'edizione con quel nome piu' vicina
-- nel tempo; fuori l'organizzatore di quell'edizione e 5366. Contano gli
-- eventi dell'anno (when_start). Chi compare a un solo evento nell'anno e'
-- di passaggio: non conta in Tutti presenti, Casa aperta e In trasferta
-- (Zoccolo duro lo pesa gia' poco per costruzione, Benvenuto misura proprio
-- il primo contatto).
-- Quota efficace, come in classifica: valore / (N_r^0,7 * Nmedia^0,3), con
-- N_r i soci attivi del registro nella regione (Nmedia per le sedi senza
-- soci nel registro) e Nmedia quelli di una regione media. Le percentuali
-- escono non arrotondate: le arrotonda il codice, per difetto, cosi' il
-- numero salvato e la soglia raggiunta dicono sempre la stessa cosa.
-- "Eventi regionali" = non nazionali che si svolgono nella regione
-- (positions.state), da chiunque organizzati. "Soci della regione" =
-- members_registry.area, con gli accorpamenti di sempre. Lo staff della
-- sede conta come socio.
SELECT
  lo.id AS id,
  lo.name AS name,
  lo.region AS region,
  CAST(CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END AS TEXT) AS region_key,
  CAST(COALESCE(reg.active, 0) AS INTEGER) AS members_active,

  -- 1 Tutti presenti: soci della regione visti ad almeno due eventi diversi
  -- nell'anno (ovunque, anche nazionali); present_once e' chi compare una
  -- volta sola, solo per vedere quanti sono
  CAST(COALESCE(pr.members, 0) AS INTEGER) AS present_members,
  CAST(COALESCE(pr.once, 0) AS INTEGER) AS present_once,
  CAST(COALESCE(100.0 * COALESCE(pr.members, 0) / COALESCE(reg.den, nb.avg_active), 0) AS REAL) AS present_pct,

  -- 2 Casa aperta: regioni diverse da cui arrivano agli eventi regionali i soci
  -- visti almeno due volte nell'anno
  CAST(COALESCE(ho.regions, 0) AS INTEGER) AS host_regions,
  CAST(COALESCE(ho.visitors, 0) AS INTEGER) AS host_visitors,

  -- 3 In trasferta: regioni diverse dove i soci della regione visti almeno due
  -- volte nell'anno hanno timbrato (eventi regionali altrui)
  CAST(COALESCE(aw.regions, 0) AS INTEGER) AS away_regions,
  CAST(COALESCE(aw.members, 0) AS INTEGER) AS away_members,

  -- 4 Senza pause: mesi dell'anno con almeno un evento regionale timbrato
  CAST(COALESCE(hm.months, 0) AS INTEGER) AS active_months,

  -- 5 Benvenuto: iscritti all'app nell'anno (users.created) con un timbro entro 60 giorni
  CAST(COALESCE(nw.newcomers, 0) AS INTEGER) AS newcomers,
  CAST(COALESCE(nw.stamped, 0) AS INTEGER) AS newcomers_stamped,
  CAST(100.0 * COALESCE(nw.stamped, 0) / MAX(COALESCE(nw.newcomers, 0), 1) AS REAL) AS newcomers_pct,

  -- 6 Sala piena: miglior evento regionale dell'anno, timbratori
  CAST(COALESCE(be.claimers, 0) AS INTEGER) AS best_event_claimers,
  CAST(COALESCE(100.0 * COALESCE(be.claimers, 0) / COALESCE(reg.den, nb.avg_active), 0) AS REAL) AS best_event_pct,

  -- 7 Zoccolo duro: soci effettivi agli eventi regionali, (somma timbri)^2 / somma(timbri^2)
  CAST(COALESCE(ef.persons, 0) AS INTEGER) AS core_persons,
  CAST(ROUND(COALESCE(ef.effective, 0), 1) AS REAL) AS core_effective,
  CAST(COALESCE(100.0 * COALESCE(ef.effective, 0) / COALESCE(reg.den, nb.avg_active), 0) AS REAL) AS core_pct,

  -- 8 Padroni di casa: nazionali ospitati nella regione con timbratori
  CAST(COALESCE(na.events, 0) AS INTEGER) AS national_hosted,

  -- 9 Staffetta: organizzatori diversi di eventi regionali timbrati
  CAST(COALESCE(hm.organizers, 0) AS INTEGER) AS organizers,
  CAST(COALESCE(hm.events, 0) AS INTEGER) AS regional_events,

  -- 10 Podio: posizione e punti di adesso nella classifica regioni
  CAST(COALESCE(lb.position, 0) AS INTEGER) AS podium_position,
  CAST(COALESCE(lb.score, 0) AS INTEGER) AS podium_score

FROM local_offices lo
CROSS JOIN (SELECT AVG(active) AS avg_active FROM (
        SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
        WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
        GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)) nb
LEFT JOIN (
  -- Soci attivi per regione e denominatore della quota efficace.
  SELECT r.region, r.active, POWER(r.active, 0.7) * POWER(nb.avg_active, 0.3) AS den
  FROM (
    SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region, COUNT(*) * 1.0 AS active
    FROM members_registry mr
    WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
    GROUP BY 1
  ) r
  CROSS JOIN (SELECT AVG(active) AS avg_active FROM (
        SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
        WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
        GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)) nb
) reg ON reg.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- 1: soci della regione per numero di eventi diversi a cui sono stati visti
  -- nell'anno: almeno due = presenti, uno solo = di passaggio
  SELECT region, SUM(events >= 2) AS members, SUM(events = 1) AS once
  FROM (
    SELECT user_region AS region, user, COUNT(DISTINCT event_id) AS events
    FROM (
      SELECT x.user, x.event_id, x.owner, x.is_national, x.when_start, x.su_created,
        CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS ev_region,
        CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS user_region
      FROM (
        SELECT su.user, su.created AS su_created, e.id AS event_id, e.owner, COALESCE(e.is_national, 0) AS is_national, e.when_start, e.position,
          ROW_NUMBER() OVER (PARTITION BY su.id ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
        FROM stamp_users su
        JOIN stamp st ON st.id = su.stamp
        JOIN events e ON e.name = st.description
        WHERE su.user != '' AND su.user != '5366'
      ) x
      LEFT JOIN positions p ON p.id = x.position
      LEFT JOIN members_registry mr ON mr.id = x.user
      WHERE x.nearest = 1 AND x.owner != x.user AND x.when_start != ''
        AND STRFTIME('%Y', x.when_start) = {:year}
    ) a
    WHERE a.user_region IS NOT NULL
    GROUP BY 1, 2
  )
  GROUP BY 1
) pr ON pr.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- 2: visitatori da altre regioni agli eventi regionali, solo soci visti
  -- almeno due volte nell'anno (ovunque)
  SELECT ev_region AS region, COUNT(DISTINCT user_region) AS regions, COUNT(DISTINCT user) AS visitors
  FROM (
      SELECT x.user, x.event_id, x.owner, x.is_national, x.when_start, x.su_created,
        CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS ev_region,
        CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS user_region
      FROM (
        SELECT su.user, su.created AS su_created, e.id AS event_id, e.owner, COALESCE(e.is_national, 0) AS is_national, e.when_start, e.position,
          ROW_NUMBER() OVER (PARTITION BY su.id ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
        FROM stamp_users su
        JOIN stamp st ON st.id = su.stamp
        JOIN events e ON e.name = st.description
        WHERE su.user != '' AND su.user != '5366'
      ) x
      LEFT JOIN positions p ON p.id = x.position
      LEFT JOIN members_registry mr ON mr.id = x.user
      WHERE x.nearest = 1 AND x.owner != x.user AND x.when_start != ''
        AND STRFTIME('%Y', x.when_start) = {:year}
    ) a
  WHERE NOT a.is_national AND a.ev_region IS NOT NULL AND a.user_region IS NOT NULL AND a.user_region != a.ev_region
    AND a.user IN (SELECT user FROM (
      SELECT x.user, x.event_id, x.owner, x.is_national, x.when_start, x.su_created,
        CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS ev_region,
        CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS user_region
      FROM (
        SELECT su.user, su.created AS su_created, e.id AS event_id, e.owner, COALESCE(e.is_national, 0) AS is_national, e.when_start, e.position,
          ROW_NUMBER() OVER (PARTITION BY su.id ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
        FROM stamp_users su
        JOIN stamp st ON st.id = su.stamp
        JOIN events e ON e.name = st.description
        WHERE su.user != '' AND su.user != '5366'
      ) x
      LEFT JOIN positions p ON p.id = x.position
      LEFT JOIN members_registry mr ON mr.id = x.user
      WHERE x.nearest = 1 AND x.owner != x.user AND x.when_start != ''
        AND STRFTIME('%Y', x.when_start) = {:year}
    ) k GROUP BY user HAVING COUNT(DISTINCT event_id) >= 2)
  GROUP BY 1
) ho ON ho.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- 3: soci della regione visti almeno due volte nell'anno a eventi regionali
  -- di altre regioni
  SELECT user_region AS region, COUNT(DISTINCT ev_region) AS regions, COUNT(DISTINCT user) AS members
  FROM (
      SELECT x.user, x.event_id, x.owner, x.is_national, x.when_start, x.su_created,
        CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS ev_region,
        CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS user_region
      FROM (
        SELECT su.user, su.created AS su_created, e.id AS event_id, e.owner, COALESCE(e.is_national, 0) AS is_national, e.when_start, e.position,
          ROW_NUMBER() OVER (PARTITION BY su.id ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
        FROM stamp_users su
        JOIN stamp st ON st.id = su.stamp
        JOIN events e ON e.name = st.description
        WHERE su.user != '' AND su.user != '5366'
      ) x
      LEFT JOIN positions p ON p.id = x.position
      LEFT JOIN members_registry mr ON mr.id = x.user
      WHERE x.nearest = 1 AND x.owner != x.user AND x.when_start != ''
        AND STRFTIME('%Y', x.when_start) = {:year}
    ) a
  WHERE NOT a.is_national AND a.ev_region IS NOT NULL AND a.user_region IS NOT NULL AND a.user_region != a.ev_region
    AND a.user IN (SELECT user FROM (
      SELECT x.user, x.event_id, x.owner, x.is_national, x.when_start, x.su_created,
        CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS ev_region,
        CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS user_region
      FROM (
        SELECT su.user, su.created AS su_created, e.id AS event_id, e.owner, COALESCE(e.is_national, 0) AS is_national, e.when_start, e.position,
          ROW_NUMBER() OVER (PARTITION BY su.id ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
        FROM stamp_users su
        JOIN stamp st ON st.id = su.stamp
        JOIN events e ON e.name = st.description
        WHERE su.user != '' AND su.user != '5366'
      ) x
      LEFT JOIN positions p ON p.id = x.position
      LEFT JOIN members_registry mr ON mr.id = x.user
      WHERE x.nearest = 1 AND x.owner != x.user AND x.when_start != ''
        AND STRFTIME('%Y', x.when_start) = {:year}
    ) k GROUP BY user HAVING COUNT(DISTINCT event_id) >= 2)
  GROUP BY 1
) aw ON aw.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- 4 e 9: mesi, organizzatori ed eventi regionali timbrati
  SELECT ev_region AS region,
         COUNT(DISTINCT STRFTIME('%m', when_start)) AS months,
         COUNT(DISTINCT owner) AS organizers,
         COUNT(DISTINCT event_id) AS events
  FROM (
      SELECT x.user, x.event_id, x.owner, x.is_national, x.when_start, x.su_created,
        CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS ev_region,
        CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS user_region
      FROM (
        SELECT su.user, su.created AS su_created, e.id AS event_id, e.owner, COALESCE(e.is_national, 0) AS is_national, e.when_start, e.position,
          ROW_NUMBER() OVER (PARTITION BY su.id ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
        FROM stamp_users su
        JOIN stamp st ON st.id = su.stamp
        JOIN events e ON e.name = st.description
        WHERE su.user != '' AND su.user != '5366'
      ) x
      LEFT JOIN positions p ON p.id = x.position
      LEFT JOIN members_registry mr ON mr.id = x.user
      WHERE x.nearest = 1 AND x.owner != x.user AND x.when_start != ''
        AND STRFTIME('%Y', x.when_start) = {:year}
    ) a
  WHERE NOT a.is_national AND a.ev_region IS NOT NULL
  GROUP BY 1
) hm ON hm.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- 5: iscritti all'app nell'anno, per regione, e quanti hanno un timbro entro 60 giorni
  SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
         COUNT(*) AS newcomers,
         SUM(EXISTS (SELECT 1 FROM stamp_users su WHERE su.user = u.id AND su.user != '5366'
                     AND julianday(su.created) - julianday(u.created) <= 60)) AS stamped
  FROM users u
  JOIN members_registry mr ON mr.id = u.id
  WHERE mr.area NOT IN ('', 'NaN') AND STRFTIME('%Y', u.created) = {:year}
  GROUP BY 1
) nw ON nw.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- 6: l'evento regionale dell'anno con piu' timbratori
  SELECT region, MAX(claimers) AS claimers
  FROM (
    SELECT ev_region AS region, event_id, COUNT(*) AS claimers
    FROM (
      SELECT x.user, x.event_id, x.owner, x.is_national, x.when_start, x.su_created,
        CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS ev_region,
        CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS user_region
      FROM (
        SELECT su.user, su.created AS su_created, e.id AS event_id, e.owner, COALESCE(e.is_national, 0) AS is_national, e.when_start, e.position,
          ROW_NUMBER() OVER (PARTITION BY su.id ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
        FROM stamp_users su
        JOIN stamp st ON st.id = su.stamp
        JOIN events e ON e.name = st.description
        WHERE su.user != '' AND su.user != '5366'
      ) x
      LEFT JOIN positions p ON p.id = x.position
      LEFT JOIN members_registry mr ON mr.id = x.user
      WHERE x.nearest = 1 AND x.owner != x.user AND x.when_start != ''
        AND STRFTIME('%Y', x.when_start) = {:year}
    ) a
    WHERE NOT a.is_national AND a.ev_region IS NOT NULL
    GROUP BY 1, 2
  )
  GROUP BY 1
) be ON be.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- 7: soci effettivi agli eventi regionali dell'anno
  SELECT region, COUNT(*) AS persons, (SUM(n) * SUM(n) * 1.0) / SUM(n * n) AS effective
  FROM (
    SELECT ev_region AS region, user, COUNT(*) AS n
    FROM (
      SELECT x.user, x.event_id, x.owner, x.is_national, x.when_start, x.su_created,
        CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS ev_region,
        CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS user_region
      FROM (
        SELECT su.user, su.created AS su_created, e.id AS event_id, e.owner, COALESCE(e.is_national, 0) AS is_national, e.when_start, e.position,
          ROW_NUMBER() OVER (PARTITION BY su.id ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
        FROM stamp_users su
        JOIN stamp st ON st.id = su.stamp
        JOIN events e ON e.name = st.description
        WHERE su.user != '' AND su.user != '5366'
      ) x
      LEFT JOIN positions p ON p.id = x.position
      LEFT JOIN members_registry mr ON mr.id = x.user
      WHERE x.nearest = 1 AND x.owner != x.user AND x.when_start != ''
        AND STRFTIME('%Y', x.when_start) = {:year}
    ) a
    WHERE NOT a.is_national AND a.ev_region IS NOT NULL
    GROUP BY 1, 2
  )
  GROUP BY 1
) ef ON ef.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- 8: nazionali ospitati con timbratori
  SELECT ev_region AS region, COUNT(DISTINCT event_id) AS events
  FROM (
      SELECT x.user, x.event_id, x.owner, x.is_national, x.when_start, x.su_created,
        CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS ev_region,
        CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS user_region
      FROM (
        SELECT su.user, su.created AS su_created, e.id AS event_id, e.owner, COALESCE(e.is_national, 0) AS is_national, e.when_start, e.position,
          ROW_NUMBER() OVER (PARTITION BY su.id ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
        FROM stamp_users su
        JOIN stamp st ON st.id = su.stamp
        JOIN events e ON e.name = st.description
        WHERE su.user != '' AND su.user != '5366'
      ) x
      LEFT JOIN positions p ON p.id = x.position
      LEFT JOIN members_registry mr ON mr.id = x.user
      WHERE x.nearest = 1 AND x.owner != x.user AND x.when_start != ''
        AND STRFTIME('%Y', x.when_start) = {:year}
    ) a
  WHERE a.is_national AND a.ev_region IS NOT NULL
  GROUP BY 1
) na ON na.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN view_leaderboard_regioni lb ON lb.id = lo.id
