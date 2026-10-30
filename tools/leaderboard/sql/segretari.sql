-- Classifica dei segretari regionali (local_offices_admins.is_the_officer):
-- ogni segretario vale quanto la sua regione in view_leaderboard_regioni
-- (partecipazione x comunita' + nazionali ospitati, vedi li'). Dove una sede ha due
-- segretari, hanno lo stesso punteggio e la stessa posizione. Va creata dopo
-- view_leaderboard_regioni.
SELECT
  u.id AS id,
  u.name AS name,
  r.id AS local_office,
  r.region AS region,
  -- come in view_leaderboard_regioni: a pari punti pari posizione,
  -- DENSE_RANK per avere sempre le tre medaglie
  CAST(DENSE_RANK() OVER (ORDER BY r.score DESC) AS INTEGER) AS position,
  CAST(r.score AS INTEGER) AS score,
  CAST(r.participation AS INTEGER) AS participation,
  CAST(r.community_pct AS INTEGER) AS community_pct,
  CAST(r.national_points AS INTEGER) AS national_points,
  CAST(r.national_events AS INTEGER) AS national_events,
  CAST(r.effective_attendees AS INTEGER) AS effective_attendees,
  CAST(r.distinct_attendees AS INTEGER) AS distinct_attendees,
  CAST(r.core_pct AS INTEGER) AS core_pct,
  CAST(r.reach_pct AS INTEGER) AS reach_pct,
  CAST(r.events_total AS INTEGER) AS events_total,
  CAST(r.events_scoring AS INTEGER) AS events_scoring,
  CAST(r.attendance_total AS INTEGER) AS attendance_total,
  CAST(r.members_active AS INTEGER) AS members_active,
  CAST(r.avg_share_pct AS INTEGER) AS avg_share_pct
FROM local_offices_admins la
JOIN users u ON u.id = la.user
JOIN view_leaderboard_regioni r ON r.id = la.local_office
WHERE la.is_the_officer
