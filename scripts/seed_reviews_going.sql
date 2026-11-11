-- Seed real rows for event reviews and "others are going" until the product has
-- public attendee/review flows. Safe to re-run: ids are derived from event ids
-- and unique constraints stop duplicates.
INSERT INTO event_going (event_id, user_id, display_name, created_at)
SELECT e.id,
       md5(e.id::text || n.name)::uuid,
       n.name,
       now() - (n.days_ago || ' days')::interval
FROM events e
CROSS JOIN LATERAL (
    SELECT v.name, v.days_ago
    FROM (VALUES
        ('Rani Putri', 12),
        ('Bagas Pratama', 9),
        ('Sinta Maharani', 8),
        ('Dimas Saputra', 6),
        ('Nadia Kusuma', 5),
        ('Fajar Nugroho', 4),
        ('Ayu Lestari', 3),
        ('Reza Maulana', 2),
        ('Tika Anggraini', 1),
        ('Yoga Pratama', 0)
    ) AS v(name, days_ago)
    ORDER BY hashtext(e.id::text || v.name)
    LIMIT (3 + (abs(hashtext(e.id::text)) % 7))
) n
WHERE e.deleted_at IS NULL
  AND e.status IN ('published', 'ongoing', 'completed')
ON CONFLICT (event_id, user_id) DO NOTHING;

INSERT INTO event_reviews (id, event_id, user_id, user_name, rating, body, photo_url, helpful_count, created_at)
SELECT md5(e.id::text || r.user_name || '-review')::uuid,
       e.id,
       md5(e.id::text || r.user_name || '-user')::uuid,
       r.user_name,
       r.rating,
       r.body,
       CASE WHEN hashtext(e.id::text || r.user_name) % 3 = 0 THEN r.photo_url ELSE '' END,
       r.helpful,
       now() - (r.days_ago || ' days')::interval
FROM events e
CROSS JOIN LATERAL (
    SELECT v.user_name, v.rating, v.body, v.photo_url, v.helpful, v.days_ago
    FROM (VALUES
        ('Rani Putri', 5,
         'Suasananya rapi dan tertata sejak masuk. Antrean masuk cepat, sound system jernih, dan panitia responsif waktu saya tanya lokasi kursi. Pasti balik lagi untuk event berikutnya.',
         'https://images.unsplash.com/photo-1492684223066-81342ee5ff30?auto=format&fit=crop&w=900&q=80', 34, 2),
        ('Bagas Pratama', 4,
         'Harga tiketnya masih masuk akal untuk kualitas acara seperti ini. Venue bersih, makanan di area sekitar juga lengkap. Hanya parkirnya agak penuh kalau datang menjelang mulai.',
         'https://images.unsplash.com/photo-1501386761578-eac5c94b800a?auto=format&fit=crop&w=900&q=80', 21, 4),
        ('Sinta Maharani', 5,
         'Datang sendiri tapi tetap nyaman. Banyak komunitas yang ramah dan area duduknya jelas. Acara mulai tepat waktu dan tidak terasa bertele-tele.',
         '', 18, 6),
        ('Dimas Saputra', 4,
         'Secara keseluruhan seru. Informasi lokasi dan jadwal di halaman event akurat, jadi saya tidak bingung saat tiba. Semoga ke depan ada lebih banyak pilihan kursi.',
         'https://images.unsplash.com/photo-1470229722913-7c0e2dbbafd3?auto=format&fit=crop&w=900&q=80', 12, 8),
        ('Nadia Kusuma', 5,
         'Salah satu acara paling berkesan tahun ini. Detail kecil seperti area istirahat dan petunjuk arah dipikirkan dengan baik. Kru dan relawan juga sangat membantu.',
         '', 27, 10),
        ('Fajar Nugroho', 3,
         'Acaranya oke, tapi jadwal beberapa sesi mundur sekitar 20 menit. Untung lokasinya mudah dijangkau dan cuaca mendukung. Tetap worth it untuk dicoba lagi.',
         '', 7, 13),
        ('Ayu Lestari', 5,
         'Ambience-nya hangat dan tidak terlalu padat. Saya suka karena penyelenggara benar-benar menjaga kebersihan sampai acara selesai. Recommended untuk akhir pekan.',
         'https://images.unsplash.com/photo-1524368535928-5b5e00ddc76b?auto=format&fit=crop&w=900&q=80', 15, 15),
        ('Reza Maulana', 4,
         'Pengalaman pertama datang ke event ini dan cukup puas. Proses tukar tiket cepat, kursi nyaman, dan tampilan panggungnya menarik. Akan pertimbangkan event berikutnya.',
         '', 9, 18),
        ('Tika Anggraini', 5,
         'Ramah untuk keluarga. Anak-anak senang, area food truck tertata, dan toilet bersih. Harga tiket sesuai dengan fasilitas yang didapat.',
         '', 11, 21),
        ('Yoga Pratama', 4,
         'Komunikasinya jelas dari sebelum hari H sampai acara selesai. Saya suka detail rundown yang dibagikan. Sedikit saran: tambah titik isi ulang air minum.',
         'https://images.unsplash.com/photo-1459749411175-04bf5292ceea?auto=format&fit=crop&w=900&q=80', 19, 24)
    ) AS v(user_name, rating, body, photo_url, helpful, days_ago)
    ORDER BY hashtext(e.id::text || v.user_name)
    LIMIT (2 + (abs(hashtext(e.slug)) % 3))
) r
WHERE e.deleted_at IS NULL
  AND e.status IN ('published', 'ongoing', 'completed')
ON CONFLICT (event_id, user_id) DO NOTHING;
