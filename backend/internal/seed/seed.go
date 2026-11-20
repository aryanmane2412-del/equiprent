// Package seed loads realistic development data through the real API
// (in-process), so every record passes the same validation, upload checks,
// verification and moderation as production. It is idempotent: accounts that
// exist are signed in, listings that exist (by title) are skipped.
//
// Development and test only: it relies on the development OTP simulation
// and grants an admin role directly in the database.
package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Password is shared by every seeded account (development only).
const Password = "Tractor-Rental-2026"

// Account is a seeded login.
type Account struct {
	Role, Name, Email, Phone string
}

// Accounts lists the seeded logins (printed by cmd/seed).
var Accounts = []Account{
	{"ADMIN", "Asha Admin", "admin@equiprent.dev", "+919900000001"},
	{"CUSTOMER", "Vikram Constructions", "customer@equiprent.dev", "+919900000002"},
}

// operators work for the owner at index owner (added as members by phone;
// the seed gives them the shared password).
var operators = []struct {
	Account
	owner   int
	licence string
	skills  []string
}{
	{Account{"OPERATOR", "Suresh Jadhav", "", "+919900000201"}, 0, "MH1220190012345", []string{"backhoe-loader", "excavator"}},
	{Account{"OPERATOR", "Imran Shaikh", "", "+919900000202"}, 0, "MH1220170054321", []string{"crane", "excavator"}},
	{Account{"OPERATOR", "Prakash More", "", "+919900000203"}, 1, "MH0420180067890", []string{"excavator", "road-roller"}},
}

type owner struct {
	Account
	business, legal, city, state, pan, area string
	lat, lng                                float64
}

var owners = []owner{
	{Account{"EQUIPMENT_OWNER", "Ravi Patil", "owner.pune@equiprent.dev", "+919900000101"},
		"Patil Earthmovers", "Patil Earthmovers Private Limited", "Pune", "27", "AAPCP4471K", "Chakan MIDC, Pune", 18.7606, 73.8636},
	{Account{"EQUIPMENT_OWNER", "Sunita Shinde", "owner.mumbai@equiprent.dev", "+919900000102"},
		"Shree Ganesh Infra Equipments", "Shree Ganesh Infra Equipments Private Limited", "Mumbai", "27", "AAKCS2208M", "Taloja MIDC, Navi Mumbai", 19.0607, 73.1214},
	{Account{"EQUIPMENT_OWNER", "Kiran Gowda", "owner.bengaluru@equiprent.dev", "+919900000103"},
		"Kaveri Crane Services", "Kaveri Crane Services Private Limited", "Bengaluru", "29", "AAHCK9031Q", "Peenya Industrial Area, Bengaluru", 13.0285, 77.5197},
	{Account{"EQUIPMENT_OWNER", "Srinivas Reddy", "owner.hyderabad@equiprent.dev", "+919900000104"},
		"Deccan Machinery Hire", "Deccan Machinery Hire Private Limited", "Hyderabad", "36", "AAFCD5126R", "Medchal, Hyderabad", 17.6297, 78.4814},
	{Account{"EQUIPMENT_OWNER", "Hetal Shah", "owner.ahmedabad@equiprent.dev", "+919900000105"},
		"Sabarmati Equipment Rentals", "Sabarmati Equipment Rentals Private Limited", "Ahmedabad", "24", "AACCS7740L", "Changodar, Ahmedabad", 22.9264, 72.4393},
	// Discovery data: more Pune and Mumbai localities and other cities.
	{Account{"EQUIPMENT_OWNER", "Amol Jadhav", "owner.hadapsar@equiprent.dev", "+919900000106"},
		"Sahyadri Infra Rentals", "Sahyadri Infra Rentals Private Limited", "Pune", "27", "AAJCS3318P", "Hadapsar, Pune", 18.5089, 73.9260},
	{Account{"EQUIPMENT_OWNER", "Farhan Qureshi", "owner.andheri@equiprent.dev", "+919900000107"},
		"Konkan Lifting and Earthmoving", "Konkan Lifting and Earthmoving Private Limited", "Mumbai", "27", "AAKCK6624D", "Andheri East, Mumbai", 19.1136, 72.8697},
	{Account{"EQUIPMENT_OWNER", "Prakash Wagh", "owner.nashik@equiprent.dev", "+919900000108"},
		"Godavari Equipment Hire", "Godavari Equipment Hire Private Limited", "Nashik", "27", "AAGCG2051H", "Satpur MIDC, Nashik", 20.0100, 73.7300},
	{Account{"EQUIPMENT_OWNER", "Nitin Deshmukh", "owner.nagpur@equiprent.dev", "+919900000109"},
		"Vidarbha Earthmovers", "Vidarbha Earthmovers Private Limited", "Nagpur", "27", "AAHCV8873K", "Hingna MIDC, Nagpur", 21.0900, 78.9800},
	{Account{"EQUIPMENT_OWNER", "Gurpreet Sethi", "owner.delhi@equiprent.dev", "+919900000110"},
		"Yamuna Construction Equipment", "Yamuna Construction Equipment Private Limited", "Delhi", "07", "AAFCY4410M", "Okhla Industrial Area, Delhi", 28.5355, 77.2732},
	{Account{"EQUIPMENT_OWNER", "Lakshmi Narayanan", "owner.chennai@equiprent.dev", "+919900000111"},
		"Coromandel Cranes and Earthmovers", "Coromandel Cranes and Earthmovers Private Limited", "Chennai", "33", "AACCC5927N", "Ambattur, Chennai", 13.1143, 80.1548},
}

// ownerPincodes are the business-address PIN codes, by owner index.
var ownerPincodes = []string{"410501", "410208", "560058", "501401", "382213", "411028", "400069", "422007", "440016", "110020", "600058"}

// ratings give some machines review history so rating filters and sorting
// have data (set directly; real ratings come from completed rentals).
var ratings = map[string]struct {
	avg   float64
	count int
}{
	"JCB 3DX Super backhoe loader with operator": {4.8, 37}, "Tata Hitachi EX200 excavator (20 t)": {4.5, 22},
	"BharatBenz 2828C tipper, 25 t": {4.1, 9}, "ACE NX 360 pick-and-carry crane, 14 t": {4.7, 41},
	"Godrej GX 300 diesel forklift, 3 t": {3.9, 12}, "Komatsu PC210 excavator (21 t)": {4.6, 18},
	"CAT D6R bulldozer": {4.2, 7}, "JCB 3DX Plus backhoe loader, Hadapsar": {4.4, 15},
	"Kobelco SK140 excavator (14 t)": {4.9, 11}, "Hyundai R140W wheeled excavator": {4.3, 6},
	"Escorts Hydra 15 crane, Thane": {4.0, 25}, "Liebherr LTM 1060 mobile crane, 60 t": {4.8, 14},
	"SANY SY215C excavator (21 t)": {4.5, 19}, "CAT 320D2 excavator (20 t)": {3.8, 5},
}

type price struct {
	unit  string
	rupee int64
	wet   bool
}

type listing struct {
	owner                     int
	category, make, model     string
	title, description, label string
	year                      int
	condition                 string
	hmr                       float64
	delivery, dry             bool
	tracked                   bool // needs a trailer to move
	prices                    []price
	specs                     map[string]float64
	status                    string // active | pending_review | draft | rejected
	dLat, dLng                float64
}

var colors = map[string]color.RGBA{
	"backhoe-loader": {245, 190, 30, 255}, "excavator": {240, 150, 30, 255}, "bulldozer": {235, 180, 40, 255},
	"crane": {225, 60, 50, 255}, "dump-truck": {60, 110, 190, 255}, "wheel-loader": {250, 200, 40, 255},
	"road-roller": {240, 170, 40, 255}, "forklift": {230, 120, 30, 255}, "concrete-mixer": {90, 150, 90, 255},
	"tractor": {200, 40, 40, 255}, "other": {70, 120, 160, 255},
}

var listings = []listing{
	{0, "backhoe-loader", "JCB", "3DX Super", "JCB 3DX Super backhoe loader with operator", "Well-maintained 3DX Super with 4-in-1 bucket and breaker line. Ideal for trenching, foundation and road work. Experienced operator included.", "Chakan MIDC, Pune", 2021, "excellent", 4210, true, false, false,
		[]price{{"hour", 1100, true}, {"day", 8500, true}, {"month", 190000, true}}, map[string]float64{"operating_weight_kg": 8070, "bucket_m3": 1.1, "dig_depth_m": 4.77, "engine_hp": 76}, "active", 0, 0},
	{0, "excavator", "Tata Hitachi", "EX 200LC", "Tata Hitachi EX200 excavator (20 t)", "20-tonne excavator with 0.9 m³ bucket and rock breaker option. GPS-tracked, serviced every 250 hours.", "Wagholi, Pune", 2020, "good", 6890, true, false, true,
		[]price{{"hour", 2000, true}, {"day", 16000, true}, {"month", 360000, true}}, map[string]float64{"operating_weight_kg": 20000, "bucket_m3": 0.9, "dig_depth_m": 6.7, "reach_m": 9.9}, "active", 0.08, 0.12},
	{0, "dump-truck", "BharatBenz", "2828C", "BharatBenz 2828C tipper, 25 t", "16 m³ rock body tipper for muck and aggregate haulage. Driver and diesel arrangements negotiable.", "Hinjewadi, Pune", 2022, "excellent", 52000, true, true, false,
		[]price{{"day", 7000, true}, {"day", 5500, false}, {"month", 160000, true}}, map[string]float64{"payload_t": 25, "body_volume_m3": 16}, "active", -0.18, -0.12},
	{0, "tractor", "Mahindra", "Arjun 605 DI", "Mahindra Arjun 605 tractor with trolley", "57 HP tractor with 5-tonne hydraulic trolley. Good for site clearing, water tanker and material shifting.", "Talegaon Dabhade, Pune", 2019, "good", 3100, true, true, false,
		[]price{{"hour", 700, true}, {"day", 4500, true}, {"day", 3200, false}}, map[string]float64{"engine_hp": 57}, "pending_review", -0.02, -0.17},
	{1, "crane", "ACE", "NX 360", "ACE NX 360 pick-and-carry crane, 14 t", "14-tonne hydra crane for plant erection and material shifting. Licensed operator and rigger available.", "Taloja MIDC, Navi Mumbai", 2021, "excellent", 5400, true, false, false,
		[]price{{"hour", 1800, true}, {"day", 14000, true}, {"month", 320000, true}}, map[string]float64{"max_capacity_t": 14, "boom_length_m": 17.5, "lift_height_m": 18}, "active", 0, 0},
	{1, "concrete-mixer", "Schwing Stetter", "AM 6 C", "Schwing Stetter 6 m³ transit mixer", "6 m³ transit mixer on a 4-axle chassis. Daily and monthly hire for RMC and site batching.", "Bhiwandi, Thane", 2020, "good", 61000, true, false, false,
		[]price{{"day", 6500, true}, {"month", 145000, true}}, map[string]float64{"drum_capacity_m3": 6}, "active", 0.24, -0.06},
	{1, "forklift", "Godrej", "GX 300 Diesel", "Godrej GX 300 diesel forklift, 3 t", "3-tonne diesel forklift for warehouses and yards. Side shifter, solid tyres.", "Turbhe MIDC, Navi Mumbai", 2022, "excellent", 1800, true, true, false,
		[]price{{"hour", 800, true}, {"day", 5000, true}, {"month", 85000, false}}, map[string]float64{"max_capacity_kg": 3000, "lift_height_m": 3}, "active", -0.02, -0.08},
	{1, "backhoe-loader", "CAT", "424", "CAT 424 backhoe loader", "Reliable 424 backhoe with extendable dipper. Available with operator for municipal and pipeline work.", "Panvel, Navi Mumbai", 2018, "fair", 9800, true, false, false,
		[]price{{"hour", 1000, true}, {"day", 7800, true}}, map[string]float64{"operating_weight_kg": 7930, "bucket_m3": 1.0, "dig_depth_m": 4.4}, "rejected", -0.07, 0.0},
	{2, "crane", "Tata Hitachi", "TMX 20", "Tata Hitachi TMX 20 crawler crane", "Lattice boom crawler crane for heavy lifts on metro and industrial projects. Assembly crew available.", "Peenya Industrial Area, Bengaluru", 2017, "good", 12500, false, false, true,
		[]price{{"day", 45000, true}, {"month", 950000, true}}, map[string]float64{"max_capacity_t": 50, "boom_length_m": 42, "lift_height_m": 44}, "active", 0, 0},
	{2, "excavator", "Komatsu", "PC210", "Komatsu PC210 excavator (21 t)", "PC210 with long-reach option. Low fuel consumption, ideal for lake and canal desilting.", "Hosur Road, Bengaluru", 2019, "good", 8200, true, false, true,
		[]price{{"hour", 2100, true}, {"day", 16500, true}}, map[string]float64{"operating_weight_kg": 21000, "bucket_m3": 1.0, "dig_depth_m": 6.6}, "active", -0.14, 0.06},
	{2, "road-roller", "Dynapac", "CA2500D", "Dynapac CA2500D vibratory roller, 11 t", "Single-drum soil compactor for road and highway earthwork.", "Nelamangala, Bengaluru", 2020, "excellent", 3900, true, false, true,
		[]price{{"hour", 1200, true}, {"day", 9000, true}}, map[string]float64{"operating_weight_kg": 11000, "drum_width_mm": 2130}, "active", 0.07, -0.14},
	{2, "other", "Kirloskar", "125 kVA DG Set", "Kirloskar 125 kVA silent generator", "Silent diesel generator on skid with cables and changeover. For sites, events and shutdowns.", "Electronic City, Bengaluru", 2021, "excellent", 2600, true, true, false,
		[]price{{"day", 4000, false}, {"month", 70000, false}}, map[string]float64{}, "active", -0.18, 0.07},
	{3, "bulldozer", "CAT", "D6R", "CAT D6R bulldozer", "D6R dozer with semi-U blade and ripper. Mining and land-levelling contracts welcome.", "Medchal, Hyderabad", 2016, "fair", 14300, false, false, true,
		[]price{{"hour", 3000, true}, {"day", 24000, true}}, map[string]float64{"operating_weight_kg": 18500, "blade_width_m": 3.3, "engine_hp": 200}, "active", 0, 0},
	{3, "wheel-loader", "Liugong", "835H", "Liugong 835H wheel loader", "1.8 m³ bucket loader for crusher and aggregate yards. Operator included.", "Shamshabad, Hyderabad", 2021, "excellent", 3700, true, false, false,
		[]price{{"hour", 1500, true}, {"day", 12000, true}}, map[string]float64{"operating_weight_kg": 10800, "bucket_m3": 1.8, "engine_hp": 125}, "active", -0.38, -0.05},
	{3, "excavator", "Hyundai", "R215", "Hyundai R215 excavator with breaker", "21.5 t excavator with hydraulic breaker for rock work and demolition.", "Patancheru, Hyderabad", 2018, "good", 10100, true, false, true,
		[]price{{"hour", 2300, true}, {"day", 18000, true}}, map[string]float64{"operating_weight_kg": 21500, "bucket_m3": 0.92}, "active", -0.1, -0.22},
	{3, "dump-truck", "Ashok Leyland", "2823 Tipper", "Ashok Leyland 2823 tipper", "Box-body tipper for city debris and soil haulage.", "Kompally, Hyderabad", 2017, "needs_repair", 140000, true, true, false,
		[]price{{"day", 6000, true}, {"day", 4500, false}}, map[string]float64{"payload_t": 22, "body_volume_m3": 14}, "draft", -0.08, 0.0},
	{4, "backhoe-loader", "JCB", "3DX Xtra", "JCB 3DX Xtra backhoe loader", "3DX Xtra with higher digging force. Available for long-term municipal contracts in Ahmedabad.", "Changodar, Ahmedabad", 2022, "excellent", 2100, true, false, false,
		[]price{{"hour", 1150, true}, {"day", 9000, true}, {"month", 200000, true}}, map[string]float64{"operating_weight_kg": 8150, "bucket_m3": 1.1, "dig_depth_m": 4.8}, "active", 0, 0},
	{4, "wheel-loader", "SDLG", "LG936L", "SDLG LG936L wheel loader", "Compact 1.8 m³ loader for ports, salt pans and aggregate handling.", "Sanand GIDC, Ahmedabad", 2020, "good", 5600, true, false, false,
		[]price{{"hour", 1400, true}, {"day", 11000, true}}, map[string]float64{"operating_weight_kg": 10600, "bucket_m3": 1.8}, "active", -0.05, -0.08},
	{4, "tractor", "Sonalika", "DI 750 III", "Sonalika DI 750 tractor with rotavator", "55 HP tractor with rotavator and cultivator. Farm and land-preparation work.", "Bavla, Ahmedabad", 2021, "good", 1900, true, true, false,
		[]price{{"hour", 650, true}, {"day", 4200, true}, {"day", 3000, false}}, map[string]float64{"engine_hp": 55}, "active", -0.11, -0.03},
	{4, "concrete-mixer", "Schwing Stetter", "AM 7 C", "Schwing Stetter 7 m³ transit mixer", "7 m³ mixer for RMC plants and large slabs.", "Naroda GIDC, Ahmedabad", 2019, "good", 70500, true, false, false,
		[]price{{"day", 7000, true}, {"month", 150000, true}}, map[string]float64{"drum_capacity_m3": 7}, "pending_review", 0.15, 0.2},
	{4, "forklift", "Godrej", "UNO 25 Electric", "Godrej UNO 25 electric forklift", "Battery forklift with charger for clean warehouses; no fumes.", "Vatva GIDC, Ahmedabad", 2023, "excellent", 600, true, true, false,
		[]price{{"day", 3500, false}, {"month", 60000, false}}, map[string]float64{"max_capacity_kg": 2500, "lift_height_m": 4.5}, "active", 0.02, 0.21},

	// 5: Hadapsar, Pune.
	{5, "backhoe-loader", "JCB", "3DX Plus", "JCB 3DX Plus backhoe loader, Hadapsar", "3DX Plus with hydraulic breaker. Night shifts available for metro and road projects in east Pune.", "Hadapsar, Pune", 2022, "excellent", 1650, true, false, false,
		[]price{{"hour", 1050, true}, {"day", 8200, true}, {"month", 185000, true}}, map[string]float64{"operating_weight_kg": 8070, "bucket_m3": 1.1, "dig_depth_m": 4.77}, "active", 0, 0},
	{5, "concrete-mixer", "Ajax Fiori", "ARGO 4000", "Ajax Fiori ARGO 4000 self-loading mixer", "Self-loading concrete mixer, 4 m3 per batch. Ideal for villas, rural roads and small slabs.", "Kharadi, Pune", 2021, "good", 2300, true, false, false,
		[]price{{"day", 7500, true}, {"month", 170000, true}}, map[string]float64{"drum_capacity_m3": 4}, "active", 0.0426, 0.0088},
	{5, "road-roller", "Volvo", "SD110", "Volvo SD110 soil compactor, 11 t", "Single-drum vibratory compactor for highway subgrade and industrial plots.", "Uruli Kanchan, Pune", 2019, "good", 5400, true, false, true,
		[]price{{"hour", 1300, true}, {"day", 9800, true}}, map[string]float64{"operating_weight_kg": 11200, "drum_width_mm": 2130}, "active", -0.0256, 0.2073},
	{5, "crane", "Escorts", "Hydra 14", "Escorts Hydra 14 crane, Bhosari MIDC", "14 t pick-and-carry crane for factory shifting and erection in Pimpri-Chinchwad.", "Bhosari MIDC, Pune", 2020, "good", 6100, true, false, false,
		[]price{{"hour", 1600, true}, {"day", 12500, true}}, map[string]float64{"max_capacity_t": 14, "boom_length_m": 17}, "active", 0.1111, -0.076},
	{5, "excavator", "Kobelco", "SK140", "Kobelco SK140 excavator (14 t)", "Fuel-efficient 14 t excavator, quick coupler with bucket and breaker. Narrow-site specialist.", "Katraj, Pune", 2021, "excellent", 3300, true, true, true,
		[]price{{"hour", 1700, true}, {"day", 13500, true}, {"day", 11000, false}}, map[string]float64{"operating_weight_kg": 14000, "bucket_m3": 0.6, "dig_depth_m": 5.7}, "active", -0.056, -0.061},
	// 6: Andheri, Mumbai.
	{6, "backhoe-loader", "Tata Hitachi", "Shinrai", "Tata Hitachi Shinrai backhoe loader", "Shinrai Pro backhoe for BMC drainage and road works. Operator with Mumbai experience.", "Andheri East, Mumbai", 2021, "good", 4100, true, false, false,
		[]price{{"hour", 1250, true}, {"day", 9500, true}}, map[string]float64{"operating_weight_kg": 7650, "bucket_m3": 1.0, "dig_depth_m": 4.6}, "active", 0, 0},
	{6, "excavator", "Hyundai", "R140W", "Hyundai R140W wheeled excavator", "Wheeled excavator that drives between city sites without a trailer.", "Powai, Mumbai", 2020, "good", 6900, true, false, false,
		[]price{{"hour", 2200, true}, {"day", 17500, true}}, map[string]float64{"operating_weight_kg": 14500, "bucket_m3": 0.58}, "active", 0.004, 0.0363},
	{6, "crane", "Escorts", "Hydra 15", "Escorts Hydra 15 crane, Thane", "15 t hydra crane with experienced rigger. Thane, Mulund and Ghodbunder Road.", "Thane West", 2019, "fair", 11200, true, false, false,
		[]price{{"hour", 1700, true}, {"day", 13000, true}}, map[string]float64{"max_capacity_t": 15, "boom_length_m": 17.5}, "active", 0.0836, 0.1025},
	{6, "forklift", "Godrej", "UNO 30 Electric", "Godrej UNO 30 electric forklift, Mulund", "3 t battery forklift for warehouses and malls. Charger included.", "Mulund, Mumbai", 2023, "excellent", 900, true, true, false,
		[]price{{"day", 4200, false}, {"month", 72000, false}}, map[string]float64{"max_capacity_kg": 3000, "lift_height_m": 4.5}, "active", 0.059, 0.0728},
	{6, "bulldozer", "Komatsu", "D65EX", "Komatsu D65EX bulldozer", "D65 dozer for land development and quarry work around Kalyan and Shil Phata.", "Kalyan, Thane", 2017, "fair", 15800, false, false, true,
		[]price{{"hour", 3200, true}, {"day", 25000, true}}, map[string]float64{"operating_weight_kg": 21000, "blade_width_m": 3.4, "engine_hp": 205}, "active", 0.1267, 0.2608},
	// 7: Nashik.
	{7, "backhoe-loader", "Mahindra", "EarthMaster SX", "Mahindra EarthMaster SX backhoe, Nashik", "Backhoe loader for MIDC and farm road work around Nashik.", "Satpur MIDC, Nashik", 2020, "good", 5200, true, false, false,
		[]price{{"hour", 950, true}, {"day", 7500, true}}, map[string]float64{"operating_weight_kg": 7700, "bucket_m3": 1.0}, "active", 0, 0},
	{7, "tractor", "Swaraj", "744 FE", "Swaraj 744 FE tractor with trolley, Sinnar", "48 HP tractor with tipping trolley and water tanker attachment.", "Sinnar, Nashik", 2021, "good", 2100, true, true, false,
		[]price{{"hour", 600, true}, {"day", 3800, true}, {"day", 2800, false}}, map[string]float64{"engine_hp": 48}, "active", -0.16, 0.27},
	{7, "forklift", "ACE", "AF 30D", "ACE AF 30D diesel forklift, Ambad", "3 t diesel forklift for engineering units in Ambad and Satpur.", "Ambad MIDC, Nashik", 2022, "excellent", 1500, true, true, false,
		[]price{{"day", 4600, true}, {"month", 80000, false}}, map[string]float64{"max_capacity_kg": 3000, "lift_height_m": 3}, "active", -0.05, 0.01},
	// 8: Nagpur.
	{8, "excavator", "CAT", "320D2", "CAT 320D2 excavator (20 t)", "20 t excavator for mining overburden and highway cutting in Vidarbha.", "Hingna MIDC, Nagpur", 2018, "fair", 11800, true, false, true,
		[]price{{"hour", 2000, true}, {"day", 15500, true}}, map[string]float64{"operating_weight_kg": 20500, "bucket_m3": 1.0, "dig_depth_m": 6.7}, "active", 0, 0},
	{8, "wheel-loader", "Volvo", "L90H", "Volvo L90H wheel loader", "2.7 m3 loader for coal yards and crushers at Butibori.", "Butibori MIDC, Nagpur", 2020, "good", 7300, true, false, false,
		[]price{{"hour", 1900, true}, {"day", 14500, true}}, map[string]float64{"operating_weight_kg": 15500, "bucket_m3": 2.7}, "active", -0.14, 0.02},
	{8, "dump-truck", "Tata", "Signa 2830.K", "Tata Signa 2830.K tipper, Nagpur", "Heavy-duty tipper for mineral and aggregate transport.", "Kamptee Road, Nagpur", 2021, "good", 88000, true, true, false,
		[]price{{"day", 7200, true}, {"day", 5800, false}}, map[string]float64{"payload_t": 26, "body_volume_m3": 18}, "active", 0.10, 0.12},
	// 9: Delhi NCR.
	{9, "backhoe-loader", "JCB", "3DX Eco", "JCB 3DX Eco backhoe loader, Okhla", "Fuel-saving 3DX for DDA and MCD works. Operator speaks Hindi and Punjabi.", "Okhla Industrial Area, Delhi", 2023, "excellent", 900, true, false, false,
		[]price{{"hour", 1200, true}, {"day", 9200, true}}, map[string]float64{"operating_weight_kg": 8000, "bucket_m3": 1.1}, "active", 0, 0},
	{9, "crane", "Liebherr", "LTM 1060-3.1", "Liebherr LTM 1060 mobile crane, 60 t", "All-terrain 60 t crane with 48 m boom for metro and commercial projects.", "Mundka, Delhi", 2016, "good", 9800, false, false, true,
		[]price{{"day", 65000, true}, {"month", 1350000, true}}, map[string]float64{"max_capacity_t": 60, "boom_length_m": 48}, "active", 0.1468, -0.2426},
	{9, "concrete-mixer", "Schwing Stetter", "AM 8 C", "Schwing Stetter 8 m3 transit mixer, Noida", "8 m3 mixer for high-rise pours in Noida and Greater Noida.", "Sector 63, Noida", 2022, "excellent", 30500, true, false, false,
		[]price{{"day", 7800, true}, {"month", 165000, true}}, map[string]float64{"drum_capacity_m3": 8}, "active", 0.0915, 0.1038},
	{9, "forklift", "Godrej", "GX 450 Diesel", "Godrej GX 450 diesel forklift, Manesar", "4.5 t forklift for auto component plants in Manesar.", "Manesar, Gurugram", 2021, "good", 4200, true, true, false,
		[]price{{"day", 6200, true}, {"month", 98000, false}}, map[string]float64{"max_capacity_kg": 4500, "lift_height_m": 4}, "pending_review", -0.18, -0.33},
	// 10: Chennai.
	{10, "excavator", "SANY", "SY215C", "SANY SY215C excavator (21 t)", "21 t excavator for port, metro and industrial park work.", "Ambattur, Chennai", 2021, "excellent", 4700, true, false, true,
		[]price{{"hour", 2100, true}, {"day", 16500, true}}, map[string]float64{"operating_weight_kg": 21500, "bucket_m3": 1.1}, "active", 0, 0},
	{10, "road-roller", "Hamm", "HD99", "Hamm HD99 tandem roller, Sriperumbudur", "9 t tandem roller for asphalt finishing on industrial roads.", "Sriperumbudur, Chennai", 2020, "good", 3600, true, false, true,
		[]price{{"hour", 1150, true}, {"day", 8800, true}}, map[string]float64{"operating_weight_kg": 9500, "drum_width_mm": 1680}, "active", -0.147, -0.213},
	{10, "dump-truck", "Ashok Leyland", "2825 Tipper", "Ashok Leyland 2825 tipper, Ennore", "Tipper for port and fly-ash haulage around Ennore.", "Ennore, Chennai", 2019, "fair", 120000, true, true, false,
		[]price{{"day", 6400, true}, {"day", 4900, false}}, map[string]float64{"payload_t": 24, "body_volume_m3": 16}, "active", 0.1003, 0.1655},
}

// Report summarises a run.
type Report struct {
	Accounts []Account
	Created  int
	Skipped  int
}

// Run seeds through router (the API handler) and pool (for the admin role
// grant only).
func Run(ctx context.Context, router http.Handler, pool *pgxpool.Pool, log *slog.Logger) (Report, error) {
	s := &seeder{h: router, pool: pool, log: log}
	return s.run(ctx)
}

type seeder struct {
	h    http.Handler
	pool *pgxpool.Pool
	log  *slog.Logger
}

type session struct {
	s     *seeder
	token string
	org   string
	id    string
}

type result struct {
	code int
	body map[string]any
	raw  string
}

func (r result) str(key string) string {
	cur := any(r.body)
	for k := range strings.SplitSeq(key, ".") {
		switch n := cur.(type) {
		case map[string]any:
			cur = n[k]
		case []any:
			var i int
			fmt.Sscan(k, &i)
			if i >= len(n) {
				return ""
			}
			cur = n[i]
		default:
			return ""
		}
	}
	if cur == nil {
		return ""
	}
	return fmt.Sprint(cur)
}

func (s *seeder) call(method, path, token, org string, body any, contentType string) result {
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "/api/v1"+path, rd)
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	req.RemoteAddr = "127.0.0.1:1"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if org != "" {
		req.Header.Set("X-Org-Id", org)
	}
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, req)
	out := result{code: w.Code, raw: w.Body.String()}
	_ = json.Unmarshal(w.Body.Bytes(), &out.body)
	return out
}

func (u *session) do(method, path string, body any, want int) (result, error) {
	r := u.s.call(method, path, u.token, u.org, body, "")
	if r.code != want {
		return r, fmt.Errorf("%s %s: status %d: %s", method, path, r.code, r.raw)
	}
	return r, nil
}

func (u *session) upload(purpose, name string, data []byte) (string, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("purpose", purpose)
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = w.Close()
	r := u.s.call(http.MethodPost, "/uploads", u.token, "", b.Bytes(), w.FormDataContentType())
	if r.code != http.StatusCreated {
		return "", fmt.Errorf("upload %s: status %d: %s", name, r.code, r.raw)
	}
	return r.str("id"), nil
}

// account signs in, registering (with the simulated OTP) the first time.
func (s *seeder) account(a Account, kind, org string) (*session, bool, error) {
	login := s.call(http.MethodPost, "/auth/login", "", "", map[string]any{"identifier": a.Phone, "password": Password}, "")
	created := false
	if login.code != http.StatusOK {
		body := map[string]any{"full_name": a.Name, "email": a.Email, "phone": a.Phone, "password": Password, "account_type": kind, "accept_terms": true}
		if org != "" {
			body["organization_name"] = org
		}
		reg := s.call(http.MethodPost, "/auth/register", "", "", body, "")
		if reg.code != http.StatusAccepted || reg.str("dev_otp") == "" {
			return nil, false, fmt.Errorf("register %s: status %d (is AUTH_DEV_SIMULATION on?): %s", a.Email, reg.code, reg.raw)
		}
		login = s.call(http.MethodPost, "/auth/otp/verify", "", "", map[string]any{"phone": a.Phone, "code": reg.str("dev_otp")}, "")
		if login.code != http.StatusOK {
			return nil, false, fmt.Errorf("verify %s: %s", a.Email, login.raw)
		}
		created = true
	}
	// Verify the email too (simulated link), so the account can also sign in
	// with its email address.
	if login.str("user.email_verified") != "true" {
		ev := s.call(http.MethodPost, "/auth/email/verification", login.str("access_token"), "", nil, "")
		if tok := ev.str("dev_token"); tok != "" {
			s.call(http.MethodPost, "/auth/email/verify", "", "", map[string]any{"token": tok}, "")
		}
	}
	u := &session{s: s, token: login.str("access_token"), id: login.str("user.id"), org: login.str("roles.0.organization_id")}
	return u, created, nil
}

func (s *seeder) run(ctx context.Context) (Report, error) {
	rep := Report{Accounts: append([]Account(nil), Accounts...)}

	// Admin (all platform roles via admin_super) and a customer.
	admin, _, err := s.account(Accounts[0], "customer", "")
	if err != nil {
		return rep, err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO user_roles (user_id, role_code) SELECT $1, 'admin_super'
		WHERE NOT EXISTS (SELECT 1 FROM user_roles WHERE user_id = $1 AND role_code = 'admin_super' AND revoked_at IS NULL)`, admin.id); err != nil {
		return rep, fmt.Errorf("grant admin: %w", err)
	}
	customer, created, err := s.account(Accounts[1], "customer", "")
	if err != nil {
		return rep, err
	}
	if created {
		_, _ = customer.do(http.MethodPost, "/orgs/"+customer.org+"/addresses", map[string]any{
			"label": "Hinjewadi site", "line1": "Plot 12, Phase 2, Rajiv Gandhi Infotech Park", "city": "Pune", "state_code": "27", "pincode": "411057",
			"is_site": true, "location": map[string]float64{"lat": 18.5913, "lng": 73.7389},
		}, http.StatusCreated)
	}

	cities := map[string]string{}
	cr := s.call(http.MethodGet, "/cities", "", "", nil, "")
	for _, c := range cr.body["data"].([]any) {
		m := c.(map[string]any)
		cities[fmt.Sprint(m["name"])] = fmt.Sprint(m["id"])
	}

	sessions := make([]*session, len(owners))
	for i, o := range owners {
		rep.Accounts = append(rep.Accounts, o.Account)
		u, _, err := s.account(o.Account, "equipment_owner", o.business)
		if err != nil {
			return rep, err
		}
		sessions[i] = u
		if err := s.verifyOwner(u, admin, o, i); err != nil {
			return rep, fmt.Errorf("verify %s: %w", o.business, err)
		}
	}

	for _, op := range operators {
		rep.Accounts = append(rep.Accounts, op.Account)
		if err := s.operator(sessions[op.owner], admin, op.Account, op.licence, op.skills); err != nil {
			return rep, fmt.Errorf("operator %s: %w", op.Name, err)
		}
	}

	for i, l := range listings {
		u, o := sessions[l.owner], owners[l.owner]
		ok, err := s.listing(u, admin, o, l, cities[o.city], i)
		if err != nil {
			return rep, fmt.Errorf("listing %q: %w", l.title, err)
		}
		if ok {
			rep.Created++
		} else {
			rep.Skipped++
		}
	}
	for title, r := range ratings {
		if _, err := s.pool.Exec(ctx, `UPDATE equipment SET rating_avg = $2, rating_count = $3
			WHERE title = $1 AND deleted_at IS NULL AND rating_count = 0`, title, r.avg, r.count); err != nil {
			return rep, fmt.Errorf("rating %q: %w", title, err)
		}
	}
	return rep, nil
}

// operator adds a member operator to the owner's organisation and has the
// admin approve their verification, so the owner can assign them to jobs.
func (s *seeder) operator(owner, admin *session, a Account, licence string, skills []string) error {
	add := owner.s.call(http.MethodPost, "/orgs/"+owner.org+"/members", owner.token, owner.org,
		map[string]any{"phone": a.Phone, "role": "operator", "full_name": a.Name}, "")
	if add.code != http.StatusCreated && add.code != http.StatusConflict {
		return fmt.Errorf("add member: status %d: %s", add.code, add.raw)
	}
	// Members are added by phone without a password: the first run signs in
	// with the (simulated) OTP and sets the shared seed password, so later
	// runs sign in like every other account (OTP requests have a cooldown).
	login := s.call(http.MethodPost, "/auth/login", "", "", map[string]any{"identifier": a.Phone, "password": Password}, "")
	if login.code != http.StatusOK {
		otp := s.call(http.MethodPost, "/auth/otp/request", "", "", map[string]any{"phone": a.Phone}, "")
		login = s.call(http.MethodPost, "/auth/otp/verify", "", "", map[string]any{"phone": a.Phone, "code": otp.str("dev_code")}, "")
		if login.code != http.StatusOK {
			return fmt.Errorf("operator sign-in: status %d: %s", login.code, login.raw)
		}
		set := s.call(http.MethodPost, "/auth/password/change", login.str("access_token"), "", map[string]any{"new_password": Password}, "")
		if set.code >= 300 {
			return fmt.Errorf("operator password: status %d: %s", set.code, set.raw)
		}
	}
	u := &session{s: s, token: login.str("access_token"), id: login.str("user.id")}
	v, err := u.do(http.MethodGet, "/verification/operator", nil, http.StatusOK)
	if err != nil {
		return err
	}
	switch v.str("application.status") {
	case "approved":
		return nil
	case "", "draft", "rejected":
		if _, err := u.do(http.MethodPut, "/verification/operator/profile", map[string]any{
			"licence_number": licence, "licence_class": "HTV / earthmover", "licence_expires_on": "2029-12-31",
			"experience_years": 8, "skills": skills,
			"availability": map[string]any{"days": []string{"mon", "tue", "wed", "thu", "fri", "sat"}, "start": "08:00", "end": "18:00"},
		}, http.StatusOK); err != nil {
			return err
		}
		for _, d := range []struct{ purpose, docType string }{{"operator_document", "driving_licence"}, {"identity_document", "voter_id"}} {
			id, err := u.upload(d.purpose, d.docType+".pdf", []byte(minimalPDF))
			if err != nil {
				return err
			}
			if _, err := u.do(http.MethodPost, "/verification/operator/documents", map[string]any{"doc_type": d.docType, "upload_id": id}, http.StatusCreated); err != nil {
				return err
			}
		}
		if v, err = u.do(http.MethodPost, "/verification/operator/submit", nil, http.StatusOK); err != nil {
			return err
		}
	}
	appID := v.str("id")
	if appID == "" {
		appID = v.str("application.id")
	}
	_, err = admin.do(http.MethodPost, "/admin/verifications/"+appID+"/approve", map[string]any{"reason": "Development seed"}, http.StatusOK)
	return err
}

func (s *seeder) verifyOwner(u, admin *session, o owner, i int) error {
	v, err := u.do(http.MethodGet, "/verification/owner", nil, http.StatusOK)
	if err != nil {
		return err
	}
	switch v.str("application.status") {
	case "approved":
		return nil
	case "draft", "rejected":
		if _, err := u.do(http.MethodPut, "/verification/owner/business", map[string]any{
			"account_kind": "company", "org_type": "private_limited", "legal_name": o.legal, "business_name": o.business,
			"pan": o.pan, "gstin": o.state + o.pan + "1Z" + fmt.Sprint(i%9+1), "cin": fmt.Sprintf("U45200MH20%02dPTC1%05d", 10+i, 2300+i),
			"contact_name": o.Name, "contact_phone": o.Phone, "contact_email": o.Email,
			"business_address": map[string]any{"line1": o.area, "city": o.city, "state_code": o.state, "pincode": ownerPincodes[i]},
		}, http.StatusOK); err != nil {
			return err
		}
		for _, d := range []struct{ purpose, docType string }{{"identity_document", "pan"}, {"business_document", "gst_certificate"}} {
			id, err := u.upload(d.purpose, d.docType+".pdf", []byte(minimalPDF))
			if err != nil {
				return err
			}
			if _, err := u.do(http.MethodPost, "/verification/owner/documents", map[string]any{"doc_type": d.docType, "upload_id": id}, http.StatusCreated); err != nil {
				return err
			}
		}
		acct := fmt.Sprintf("5010012345%04d", 6789+i)
		if _, err := u.do(http.MethodPut, "/verification/owner/bank-account", map[string]any{
			"account_holder_name": o.legal, "account_number": acct, "confirm_account_number": acct, "ifsc": "HDFC0001234",
		}, http.StatusOK); err != nil {
			return err
		}
		v, err = u.do(http.MethodPost, "/verification/owner/submit", nil, http.StatusOK)
		if err != nil {
			return err
		}
	}
	appID := v.str("id")
	if appID == "" {
		appID = v.str("application.id")
	}
	_, err = admin.do(http.MethodPost, "/admin/verifications/"+appID+"/approve", map[string]any{"reason": "Development seed"}, http.StatusOK)
	return err
}

// listing creates one listing unless one with the same title exists.
func (s *seeder) listing(u, admin *session, o owner, l listing, cityID string, i int) (bool, error) {
	mine, err := u.do(http.MethodGet, "/owner/equipment?limit=100", nil, http.StatusOK)
	if err != nil {
		return false, err
	}
	if strings.Contains(mine.raw, `"title":"`+l.title+`"`) {
		return false, nil
	}
	wet := false
	for _, p := range l.prices {
		wet = wet || p.wet
	}
	body := map[string]any{
		"title": l.title, "description": l.description, "category": l.category, "manufacturer": l.make, "model": l.model,
		"manufacture_year": l.year, "condition": l.condition, "current_hmr": l.hmr, "delivery_available": l.delivery,
		"location_label": l.label, "wet_hire": wet, "dry_hire": l.dry, "service_radius_km": 60,
		"base_location":   map[string]float64{"lat": o.lat + l.dLat, "lng": o.lng + l.dLng},
		"registration_no": fmt.Sprintf("%s%02d%s%04d", map[string]string{"27": "MH", "29": "KA", "36": "TS", "24": "GJ", "07": "DL", "06": "HR", "33": "TN"}[o.state], 10+i%40, "EQ", 1000+i),
	}
	if cityID != "" {
		body["city_id"] = cityID
	}
	created, err := u.do(http.MethodPost, "/owner/equipment", body, http.StatusCreated)
	if err != nil {
		return false, err
	}
	id := created.str("id")

	plans := make([]map[string]any, 0, len(l.prices))
	for _, p := range l.prices {
		plan := map[string]any{"unit": p.unit, "with_operator": p.wet, "rate_paise": p.rupee * 100}
		if p.wet && p.unit != "hour" {
			plan["operator_bata_per_day_paise"] = 50000
		}
		plans = append(plans, plan)
	}
	mob := map[string]any{"fixed_paise": 300000, "per_km_paise": 6000, "free_km": 10, "needs_trailer": l.tracked}
	if !l.delivery {
		mob = nil
	}
	if _, err := u.do(http.MethodPut, "/owner/equipment/"+id+"/pricing", map[string]any{"plans": plans, "mobilisation": mob}, http.StatusOK); err != nil {
		return false, err
	}
	if len(l.specs) > 0 {
		specs := make([]map[string]any, 0, len(l.specs))
		for k, v := range l.specs {
			specs = append(specs, map[string]any{"key": k, "value_numeric": v})
		}
		if _, err := u.do(http.MethodPut, "/owner/equipment/"+id+"/specs", map[string]any{"specs": specs}, http.StatusOK); err != nil {
			return false, err
		}
	}
	for v, angle := range []string{"front", "left", "rear"} {
		up, err := u.upload("equipment_photo", angle+".jpg", Photo(l.category, colors[l.category], i+v))
		if err != nil {
			return false, err
		}
		if _, err := u.do(http.MethodPost, "/owner/equipment/"+id+"/photos", map[string]any{"upload_id": up, "angle": angle}, http.StatusCreated); err != nil {
			return false, err
		}
	}
	if up, err := u.upload("equipment_document", "rc.pdf", []byte(minimalPDF)); err == nil {
		_, _ = u.do(http.MethodPost, "/owner/equipment/"+id+"/documents", map[string]any{"doc_type": "rc", "upload_id": up, "document_number": body["registration_no"]}, http.StatusCreated)
	}
	if up, err := u.upload("equipment_document", "insurance.pdf", []byte(minimalPDF)); err == nil {
		_, _ = u.do(http.MethodPost, "/owner/equipment/"+id+"/documents", map[string]any{"doc_type": "insurance", "upload_id": up, "valid_until": "2027-03-31"}, http.StatusCreated)
	}

	if l.status == "draft" {
		return true, nil
	}
	if _, err := u.do(http.MethodPost, "/owner/equipment/"+id+"/submit", nil, http.StatusOK); err != nil {
		return false, err
	}
	switch l.status {
	case "active":
		_, err = admin.do(http.MethodPost, "/admin/listings/"+id+"/approve", map[string]any{}, http.StatusOK)
	case "rejected":
		_, err = admin.do(http.MethodPost, "/admin/listings/"+id+"/reject", map[string]any{
			"reason": "Photos are unclear and the RC copy does not match the registration number. Please upload clear photos and the correct RC.",
		}, http.StatusOK)
	}
	return true, err
}
