-- Classifica delle regioni (una riga per sede, local_offices).
-- Punteggio = partecipazione x comunita' + nazionali ospitati.
-- Partecipazione: gli eventi non nazionali degli ultimi 12 mesi che si sono
-- svolti nella regione, da chiunque organizzati, con la stessa S della
-- classifica soci sulla quota di soci timbratori (s = timbratori /
-- (N_r^0,7 * Nmedia^0,3), con N_r i soci attivi del registro nella regione e
-- Nmedia quelli di una regione media: zero sotto il 2%, meta' all'8,5%,
-- tutto dal 15%) e la stessa scadenza lineare a 12 mesi. Ogni evento vale
-- fino a 10 punti, come la E della classifica soci.
-- Comunita': quanto e' ampia, rispetto ai soci della regione, la comunita'
-- che timbra ai suoi eventi. I soci "effettivi" sono
-- (somma timbri)^2 / somma(timbri^2): quanti sarebbero se tutti timbrassero
-- lo stesso numero di volte (vale il numero dei timbratori se ognuno timbra
-- una volta, tende a 1 se timbra sempre uno solo). La loro quota sulla
-- regione passa nella stessa S, con lo stesso denominatore: c = 1 dal 15%.
-- Nazionali ospitati: gli eventi nazionali che si sono svolti nella regione
-- negli ultimi 24 mesi, con la stessa formula (10 punti max, stessa S sui
-- timbratori, scadenza a 24 mesi come nella classifica soci) ma fuori dal
-- fattore comunita': un convegno porta gente da tutta Italia, e la curva
-- satura da sola (56-103 timbratori).
-- La classifica soci legge community_pct da qui per pesare i timbri e gli
-- eventi organizzati di ogni regione, region_key e' il nome della regione
-- come in positions.state (accorpato), per quel join.
-- Timbratori: timbro -> edizione piu' vicina nel tempo, senza l'organizzatore
-- di quell'edizione e senza 5366, l'account che prende ogni timbro.
-- Le sedi che coprono due regioni contano come una (vedi i CASE), i nomi
-- delle sedi (local_offices.region) non coincidono con positions.state e
-- vengono mappati. avg_share_pct e' la quota vera, timbratori / soci della
-- regione, in media sugli eventi.
SELECT
  lo.id AS id,
  lo.name AS name,
  lo.region AS region,
  -- PocketBase accetta come colonna solo identificatori o CAST(...): il
  -- CASE nudo non passa il parser della vista
  CAST(CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END AS TEXT) AS region_key,
  -- La posizione dipende solo dai punti mostrati: a pari punti, pari
  -- posizione. DENSE_RANK perche' le posizioni 1, 2 e 3 esistano sempre e
  -- le tre medaglie vengano assegnate anche con un pari merito. L'ordine
  -- dentro un pari merito lo decide l'app (partecipazione, poi eventi).
  CAST(DENSE_RANK() OVER (ORDER BY ROUND(COALESCE(r.score, 0) * COALESCE(d.community, 0) + COALESCE(n.score, 0)) DESC) AS INTEGER) AS position,
  CAST(ROUND(COALESCE(r.score, 0) * COALESCE(d.community, 0) + COALESCE(n.score, 0)) AS INTEGER) AS score,
  CAST(ROUND(COALESCE(r.score, 0)) AS INTEGER) AS participation,
  CAST(ROUND(100 * COALESCE(d.community, 0)) AS INTEGER) AS community_pct,
  CAST(ROUND(COALESCE(n.score, 0)) AS INTEGER) AS national_points,
  CAST(COALESCE(n.events_total, 0) AS INTEGER) AS national_events,
  CAST(ROUND(COALESCE(d.effective_attendees, 0)) AS INTEGER) AS effective_attendees,
  CAST(COALESCE(d.distinct_attendees, 0) AS INTEGER) AS distinct_attendees,
  CAST(ROUND(100.0 * COALESCE(d.effective_attendees, 0) / MAX(COALESCE(reg.active, 0), 1)) AS INTEGER) AS core_pct,
  CAST(ROUND(100.0 * COALESCE(d.distinct_attendees, 0) / MAX(COALESCE(reg.active, 0), 1)) AS INTEGER) AS reach_pct,
  CAST(COALESCE(r.events_total, 0) AS INTEGER) AS events_total,
  CAST(COALESCE(r.events_scoring, 0) AS INTEGER) AS events_scoring,
  CAST(COALESCE(r.attendance_total, 0) AS INTEGER) AS attendance_total,
  CAST(COALESCE(reg.active, 0) AS INTEGER) AS members_active,
  CAST(ROUND(100 * COALESCE(r.avg_share, 0)) AS INTEGER) AS avg_share_pct
FROM local_offices lo
LEFT JOIN (
  -- Soci attivi per regione, dal registro soci, con gli accorpamenti.
  SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
         COUNT(*) * 1.0 AS active
  FROM members_registry mr
  WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
  GROUP BY 1
) reg ON reg.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- Partecipazione: un punteggio per evento, sommato per regione.
  SELECT region,
    SUM(10.0 * decay * t * t * (3 - 2 * t)) AS score,
    COUNT(*) AS events_total,
    SUM(t > 0) AS events_scoring,
    SUM(claimers) AS attendance_total,
    AVG(share) AS avg_share
  FROM (
    SELECT x.region, x.decay, x.claimers, x.share,
      MIN(1.0, MAX(0.0, (x.share_eff - 0.02) / 0.13)) AS t
    FROM (
      SELECT CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS region,
        1 - (julianday('now') - julianday(e.when_start)) / 365 AS decay,
        COALESCE(cnt.claimers, 0) AS claimers,
        COALESCE(cnt.claimers, 0) / reg.active AS share,
        COALESCE(cnt.claimers, 0) / (POWER(reg.active, 0.7) * POWER(nb.avg_active, 0.3)) AS share_eff
      FROM events e
      JOIN positions p ON p.id = e.position
      JOIN (
        SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
               COUNT(*) * 1.0 AS active
        FROM members_registry mr
        WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
        GROUP BY 1
      ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END
      CROSS JOIN (
        -- Soci di una regione media, nel denominatore con esponente 0,3.
        SELECT AVG(active) AS avg_active FROM (
          SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
          WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
          GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
      ) nb
      LEFT JOIN (
        -- timbratori: ogni timbro preso va all'evento con quel nome piu' vicino
        -- nel tempo, non contano chi ha organizzato quell'edizione e 5366,
        -- l'account che prende ogni timbro
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
      WHERE NOT e.is_national
        AND e.when_start != ''
        AND julianday(e.when_start) <= julianday('now')
        AND julianday(e.when_start) > julianday('now', '-365 days')
    ) x
  )
  GROUP BY region
) r ON r.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- Nazionali ospitati: 10 x decay a 24 mesi x S sui timbratori, senza comunita'.
  SELECT region,
    SUM(10.0 * decay * t * t * (3 - 2 * t)) AS score,
    COUNT(*) AS events_total
  FROM (
    SELECT x.region, x.decay,
      MIN(1.0, MAX(0.0, (x.share_eff - 0.02) / 0.13)) AS t
    FROM (
      SELECT CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS region,
        1 - (julianday('now') - julianday(e.when_start)) / 730 AS decay,
        COALESCE(cnt.claimers, 0) / (POWER(reg.active, 0.7) * POWER(nb.avg_active, 0.3)) AS share_eff
      FROM events e
      JOIN positions p ON p.id = e.position
      JOIN (
        SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
               COUNT(*) * 1.0 AS active
        FROM members_registry mr
        WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
        GROUP BY 1
      ) reg ON reg.region = CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END
      CROSS JOIN (
        SELECT AVG(active) AS avg_active FROM (
          SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
          WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
          GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
      ) nb
      LEFT JOIN (
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
      WHERE e.is_national
        AND e.when_start != ''
        AND julianday(e.when_start) <= julianday('now')
        AND julianday(e.when_start) > julianday('now', '-730 days')
    ) x
  )
  GROUP BY region
) n ON n.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
LEFT JOIN (
  -- Comunita': soci effettivi tra i timbratori, in quota alla regione, nella S.
  SELECT region, distinct_attendees, effective_attendees,
    t * t * (3 - 2 * t) AS community
  FROM (
    SELECT region, distinct_attendees, effective_attendees,
      MIN(1.0, MAX(0.0, (effective_attendees / (POWER(active, 0.7) * POWER(avg_active, 0.3)) - 0.02) / 0.13)) AS t
    FROM (
      SELECT a.region,
        COUNT(*) AS distinct_attendees,
        (SUM(n) * SUM(n) * 1.0) / SUM(n * n) AS effective_attendees,
        reg.active,
        nb.avg_active
      FROM (
        -- timbri per persona agli eventi regionali degli ultimi 12 mesi
        SELECT region, user, COUNT(*) AS n
        FROM (
          SELECT x.user, CASE p.state WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE p.state END AS region
          FROM (
            SELECT su.user, e.owner, e.position, e.is_national, e.when_start,
              ROW_NUMBER() OVER (PARTITION BY su.id
                                 ORDER BY abs(julianday(su.created) - julianday(e.when_start))) AS nearest
            FROM stamp_users su
            JOIN stamp st ON st.id = su.stamp
            JOIN events e ON e.name = st.description
            WHERE su.user != '' AND su.user != '5366'
          ) x
          JOIN positions p ON p.id = x.position
          WHERE x.nearest = 1 AND x.owner != x.user AND NOT x.is_national
            AND x.when_start != ''
            AND julianday(x.when_start) <= julianday('now')
            AND julianday(x.when_start) > julianday('now', '-365 days')
        )
        GROUP BY region, user
      ) a
      JOIN (
        SELECT CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END AS region,
               COUNT(*) * 1.0 AS active
        FROM members_registry mr
        WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
        GROUP BY 1
      ) reg ON reg.region = a.region
      CROSS JOIN (
        SELECT AVG(active) AS avg_active FROM (
          SELECT COUNT(*) * 1.0 AS active FROM members_registry mr
          WHERE mr.is_active AND mr.area NOT IN ('', 'NaN')
          GROUP BY CASE mr.area WHEN 'Trentino-Alto Adige/Südtirol' THEN 'Trentino-Alto Adige' WHEN 'Valle d''Aosta' THEN 'Piemonte' WHEN 'Basilicata' THEN 'Puglia' ELSE mr.area END)
      ) nb
      GROUP BY a.region
    )
  )
) d ON d.region = CASE lo.region WHEN 'Basilicata e Puglia' THEN 'Puglia' WHEN 'Piemonte e Val d''Aosta' THEN 'Piemonte' WHEN 'Emilia Romagna' THEN 'Emilia-Romagna' WHEN 'Friuli Venezia Giulia' THEN 'Friuli-Venezia Giulia' WHEN 'Trentino Alto Adige' THEN 'Trentino-Alto Adige' ELSE lo.region END
