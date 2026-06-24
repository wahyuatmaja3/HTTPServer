# Product Requirement Document (PRD): HTTP Server DB GUI (Pixel-Perfect Specification)

## 1. Project Overview & Objective
Dokumen ini mendefinisikan spesifikasi tata letak berbasis piksel (*pixel-perfect absolute positioning*) untuk membangun ulang aplikasi desktop Windows **"HTTP Server DB"**. Target utama dari dokumen ini adalah memberikan instruksi koordinat eksak kepada AI Coding agar komponen GUI yang dihasilkan memiliki posisi, jarak (spacing), bentuk, dan hierarki visual yang tepat sama dengan gambar referensi.

---

## 2. Window Geometry & Global Settings
* **Window Title:** `HTTP Server DB`
* **Window Dimensions:** `650px` (Width) × `480px` (Height) *-- (Termasuk title bar dan border sistem)*
* **Form Background Color:** `#F0F0F0` (Standard Windows Control / `SystemColors.Control`)
* **Default Font:** `Tahoma` atau `MS Sans Serif`, `9pt`, `Regular`
* **Font Color:** `#000000` (Hitam Pekat)
* **Global Padding/Margin:** `10px` dari batas tepi luar Form untuk komponen utama.

---

## 3. Top Section: Tab Control ("Settings")
Sebuah kontainer Tab Control membentang penuh di bagian atas untuk menampung seluruh konfigurasi jaringan, parameter performa, dan metrik monitoring.

* **Koordinat Komponen:** $X = 10$, $Y = 10$
* **Ukuran Komponen:** $Width = 630$, $Height = 170$
* **Garis Batas Dalam (Client Area):** Area konten di dalam tab dimulai secara efektif pada koordinat internal $Y = 30$ (di bawah tab penyekat/label tab `Settings`).

### 3.1 Sektor 1: Jaringan & Logika Tingkat Lanjut (Sisi Kiri)
Semua koordinat di bawah ini adalah koordinat **internal relatif** terhadap bidang Tab Control.

* **Label "Bind to IPs"**
    * Koordinat: $X = 10$, $Y = 15$
    * Ukuran: $Width = 100$, $Height = 14$
* **ListBox (Daftar IP dengan Checkbox)**
    * Koordinat: $X = 10$, $Y = 32$
    * Ukuran: $Width = 130$, $Height = 100$
    * Item Default: `127.0.0.1` (Checked), `26.117.11.157`, `172.31.176.1`, `192.168.56.1`, `192.168.0.107`
    * Properti: Vertical Scrollbar otomatis aktif jika item meluap.
* **Label "Bind to port"**
    * Koordinat: $X = 150$, $Y = 15$
    * Ukuran: $Width = 80$, $Height = 14$
* **TextBox "Port"**
    * Koordinat: $X = 150$, $Y = 32$
    * Ukuran: $Width = 75$, $Height = 22$
    * Nilai Default: `8024`
* **CheckBox "Detail Log"**
    * Koordinat: $X = 150$, $Y = 110$
    * Ukuran: $Width = 90$, $Height = 20$
    * Status Default: Checked (Tercentang)

### 3.2 Sektor 2: Parameter Performa Server (Sisi Tengah)
Sektor ini menggunakan tata letak form vertikal yang sejajar. Semua Label berposisi rata kiri di $X = 250$, sedangkan seluruh TextBox berposisi rata kiri di $X = 360$ dengan lebar seragam `80px`. Jarak antar baris (*vertical pitch*) secara konsisten adalah `28px`.

| Komponen | Koordinat X | Koordinat Y | Ukuran (Width × Height) | Nilai Default |
| :--- | :--- | :--- | :--- | :--- |
| **Label "Max Connections"** | $X = 250$ | $Y = 18$ | $100 	imes 14$ | - |
| **TextBox "Max Connections"**| $X = 360$ | $Y = 15$ | $80 	imes 22$ | `100` |
| **Label "Listen Queue"** | $X = 250$ | $Y = 46$ | $100 	imes 14$ | - |
| **TextBox "Listen Queue"** | $X = 360$ | $Y = 43$ | $80 	imes 22$ | `0` |
| **Label "Session TimeOut"** | $X = 250$ | $Y = 74$ | $100 	imes 14$ | - |
| **TextBox "Session TimeOut"**| $X = 360$ | $Y = 71$ | $80 	imes 22$ | `8000` |
| **Label "MaxThreads"** | $X = 250$ | $Y = 102$ | $100 	imes 14$ | - |
| **TextBox "MaxThreads"** | $X = 360$ | $Y = 99$ | $80 	imes 22$ | `2000` |

### 3.3 Sektor 3: Real-Time Monitoring Metrics (Sisi Kanan)
Sektor ini berfungsi menampilkan performa lalu lintas server dalam kondisi *Read-Only*. Semua Label diletakkan rata kiri di $X = 460$, sedangkan seluruh TextBox diletakkan rata kiri di $X = 550$ dengan lebar seragam `70px`.

| Komponen | Koordinat X | Koordinat Y | Ukuran (Width × Height) | Status Kontrol |
| :--- | :--- | :--- | :--- | :--- |
| **Label "Total Req"** | $X = 460$ | $Y = 18$ | $80 	imes 14$ | - |
| **TextBox "Total Req"** | $X = 550$ | $Y = 15$ | $70 	imes 22$ | Read-Only / Empty |
| **Label "On Proses"** | $X = 460$ | $Y = 46$ | $80 	imes 14$ | - |
| **TextBox "On Proses"** | $X = 550$ | $Y = 43$ | $70 	imes 22$ | Read-Only / Empty |
| **Label "Max On Proses"** | $X = 460$ | $Y = 74$ | $80 	imes 14$ | - |
| **TextBox "Max On Proses"** | $X = 550$ | $Y = 71$ | $70 	imes 22$ | Read-Only / Empty |
| **Label "Max Time"** | $X = 460$ | $Y = 102$ | $80 	imes 14$ | - |
| **TextBox "Max Time"** | $X = 550$ | $Y = 99$ | $70 	imes 22$ | Read-Only / Empty |

---

## 4. Middle Section: Log Monitor Window
Penyekat horisontal berupa garis tipis 3D bevel (*etched line*) diletakkan tepat di koordinat luar $Y = 190$ membentang sepanjang `630px` dari kiri ke kanan.

* **Jenis Kontrol:** Multi-line TextBox / RichTextBox (`ScrollBars = Vertical`, `ReadOnly = True`).
* **Warna Latar Belakang:** `#FFFFFF` (Putih)
* **Koordinat Utama Window:** $X = 10$, $Y = 195$
* **Ukuran Komponen:** $Width = 630$, $Height = 230$
* **Teks Awal Saat Dijalankan:**
    ```text
    01/03/2026 22.39.32 FormCreate
    03/01/2026 22.39.33 DB Open
    03/01/2026 22.39.33 Version : 9 Maret 2024
    ```

---

## 5. Bottom Section: Action Panel
* **Garis Pembatas Bawah:** Terdapat pembatas visual horizontal samar di koordinat $Y = 435$.
* **Tombol "Start Server"**
    * Koordinat: $X = 530$, $Y = 442$
    * Ukuran: $Width = 110$, $Height = 28$
    * Gaya Visual: Tombol bergaya klasik Windows dengan *inner focus rectangle* (garis titik-titik halus mengelilingi teks "Start Server" saat aplikasi aktif).
