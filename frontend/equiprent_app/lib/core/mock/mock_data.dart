import '../theme/app_icons.dart';
import 'models.dart';

/// Deterministic design-stage data. Names, companies and numbers are
/// fictional but realistic for the Pune pilot market. Prices are in rupees.
abstract final class MockData {
  /// Fixed "today" so screens and tests are stable.
  static final now = DateTime(2026, 11, 12, 10, 30);

  static DateTime day(int offset, [int hour = 8, int minute = 0]) =>
      DateTime(now.year, now.month, now.day + offset, hour, minute);

  // ---- Personas -------------------------------------------------------------
  static const customerName = 'Ravi Deshmukh';
  static const customerCompany = 'Deshmukh Constructions';
  static const ownerName = 'Suresh Patil';
  static const ownerCompany = 'Patil Earthmovers';
  static const operatorName = 'Imran Shaikh';
  static const fleetManagerName = 'Anita Kulkarni';
  static const fleetCompany = 'Patil Infra Equipments';
  static const adminName = 'Neha Joshi';

  // ---- Catalogue --------------------------------------------------------------
  static const categories = [
    EquipmentCategory(id: 'backhoe', name: 'Backhoe Loaders', icon: AppIcons.backhoe, description: 'Digging, loading and trenching for small and mid-size sites', available: 46, fromShiftRate: 7800),
    EquipmentCategory(id: 'excavator', name: 'Excavators', icon: AppIcons.excavator, description: '7–40 tonne crawler excavators for bulk earthwork', available: 31, fromShiftRate: 16500),
    EquipmentCategory(id: 'crane', name: 'Cranes', icon: AppIcons.crane, description: 'Pick-and-carry and mobile cranes, 12–50 tonne', available: 18, fromShiftRate: 12000),
    EquipmentCategory(id: 'roller', name: 'Rollers & Compactors', icon: AppIcons.roller, description: 'Soil compactors and tandem rollers for roads and bases', available: 22, fromShiftRate: 8200),
    EquipmentCategory(id: 'mixer', name: 'Transit Mixers', icon: AppIcons.mixer, description: '6–10 m³ mixers with experienced drivers', available: 15, fromShiftRate: 7500),
    EquipmentCategory(id: 'tipper', name: 'Tippers & Dumpers', icon: AppIcons.tipper, description: '10–25 tonne tippers for muck and material shifting', available: 39, fromShiftRate: 6200),
    EquipmentCategory(id: 'telehandler', name: 'Telehandlers', icon: AppIcons.telehandler, description: 'Lifting and placing at height on building sites', available: 8, fromShiftRate: 9800),
    EquipmentCategory(id: 'dozer', name: 'Dozers & Graders', icon: AppIcons.dozer, description: 'Land clearing, levelling and road grading', available: 11, fromShiftRate: 18500),
  ];

  static EquipmentCategory category(String id) => categories.firstWhere((c) => c.id == id, orElse: () => categories.first);

  static final _reviews = [
    Review(author: 'Prakash Jadhav', company: 'Jadhav Builders', rating: 5, text: 'Machine arrived on time, operator knew the work. Hour logs matched exactly.', date: day(-9)),
    Review(author: 'Sneha Kale', company: 'Kale Infra Projects', rating: 4.5, text: 'Good condition, minor delay in mobilisation but owner informed in advance.', date: day(-21)),
    Review(author: 'Amit Bhosale', company: 'Bhosale Roadlines', rating: 4, text: 'Fair price for the shift. Would book again for our Chakan site.', date: day(-40)),
  ];

  static final equipment = <Equipment>[
    Equipment(
      id: 'eq-001', name: 'JCB 3DX Super Backhoe Loader', make: 'JCB', model: '3DX Super', categoryId: 'backhoe', year: 2022,
      ownerId: 'own-patil', ownerName: ownerCompany, area: 'Hinjewadi', city: 'Pune', distanceKm: 4.2,
      hourlyRate: 1100, shiftRate: 8500, monthlyRate: 185000, mobilisationFee: 2500, rating: 4.8, reviewCount: 126, hmr: 3460,
      instantBook: true, utilisation: 0.82, registration: 'MH 12 KX 4471', monthEarnings: 168000, reviews: _reviews,
      specs: {'Operating weight': '7,800 kg', 'Engine power': '76 HP', 'Bucket capacity': '1.1 m³', 'Dig depth': '4.77 m', 'Fuel tank': '120 L'},
      description: 'Well-maintained 2022 backhoe with experienced operator. Ideal for foundation digging, trenching and loading. Serviced every 250 hours at authorised dealer.',
    ),
    Equipment(
      id: 'eq-002', name: 'Tata Hitachi EX200 Excavator', make: 'Tata Hitachi', model: 'EX 200 LC', categoryId: 'excavator', year: 2021,
      ownerId: 'own-patil', ownerName: ownerCompany, area: 'Wakad', city: 'Pune', distanceKm: 6.8,
      hourlyRate: 2300, shiftRate: 18000, monthlyRate: 420000, mobilisationFee: 9000, rating: 4.7, reviewCount: 84, hmr: 6120,
      utilisation: 0.74, status: EquipmentStatus.onRent, registration: 'MH 14 HU 2290', monthEarnings: 312000, reviews: _reviews,
      specs: {'Operating weight': '20,500 kg', 'Engine power': '148 HP', 'Bucket capacity': '1.0 m³', 'Max dig depth': '6.67 m', 'Reach': '9.9 m'},
      description: '20-tonne class crawler excavator with rock bucket available on request. Low hours since engine overhaul.',
    ),
    Equipment(
      id: 'eq-003', name: 'CAT 320D2 Excavator', make: 'Caterpillar', model: '320D2', categoryId: 'excavator', year: 2020,
      ownerId: 'own-sahyadri', ownerName: 'Sahyadri Equipment Hire', area: 'Chakan', city: 'Pune', distanceKm: 18.5,
      hourlyRate: 2500, shiftRate: 19500, monthlyRate: 450000, mobilisationFee: 12000, rating: 4.6, reviewCount: 57, hmr: 8840,
      utilisation: 0.69, reviews: _reviews,
      specs: {'Operating weight': '21,800 kg', 'Engine power': '148 HP', 'Bucket capacity': '1.2 m³', 'Max dig depth': '6.72 m', 'Reach': '9.86 m'},
      description: 'Reliable CAT 320D2 with breaker line. GPS telematics fitted. Operator with 9 years experience.',
    ),
    Equipment(
      id: 'eq-004', name: 'ACE 14XW Pick & Carry Crane', make: 'ACE', model: '14XW', categoryId: 'crane', year: 2023,
      ownerId: 'own-patil', ownerName: ownerCompany, area: 'Kharadi', city: 'Pune', distanceKm: 12.1,
      hourlyRate: 1700, shiftRate: 13500, monthlyRate: 310000, mobilisationFee: 0, rating: 4.9, reviewCount: 41, hmr: 1880,
      instantBook: true, utilisation: 0.58, registration: 'MH 12 RT 9012', monthEarnings: 121000, reviews: _reviews,
      specs: {'Max capacity': '14 t', 'Boom length': '17.5 m', 'Max lift height': '18.2 m', 'Engine power': '76 HP', 'Road speed': '30 km/h'},
      description: 'Road-going hydra crane, drives to site under its own power. Ideal for steel erection and precast placement.',
    ),
    Equipment(
      id: 'eq-005', name: 'Case 1107EX Soil Compactor', make: 'Case', model: '1107 EX', categoryId: 'roller', year: 2021,
      ownerId: 'own-mahalaxmi', ownerName: 'Mahalaxmi Roadways', area: 'Talegaon', city: 'Pune', distanceKm: 27.4,
      hourlyRate: 1050, shiftRate: 8200, monthlyRate: 175000, mobilisationFee: 6000, rating: 4.5, reviewCount: 33, hmr: 4210,
      utilisation: 0.51, reviews: _reviews,
      specs: {'Operating weight': '11,000 kg', 'Drum width': '2,130 mm', 'Centrifugal force': '255 kN', 'Engine power': '110 HP'},
      description: '11-tonne vibratory soil compactor for road base and embankment work.',
    ),
    Equipment(
      id: 'eq-006', name: 'Schwing Stetter AM 8 Transit Mixer', make: 'Schwing Stetter', model: 'AM 8 C', categoryId: 'mixer', year: 2022,
      ownerId: 'own-sahyadri', ownerName: 'Sahyadri Equipment Hire', area: 'Moshi', city: 'Pune', distanceKm: 15.9,
      hourlyRate: 950, shiftRate: 7500, monthlyRate: 160000, mobilisationFee: 0, rating: 4.4, reviewCount: 29, hmr: 3020,
      utilisation: 0.63, reviews: _reviews,
      specs: {'Drum capacity': '8 m³', 'Chassis': 'BharatBenz 2823C', 'Water tank': '400 L', 'Drum speed': '0–14 rpm'},
      description: '8 m³ transit mixer on BharatBenz chassis with licensed HMV driver.',
    ),
    Equipment(
      id: 'eq-007', name: 'BharatBenz 2828C Tipper', make: 'BharatBenz', model: '2828C', categoryId: 'tipper', year: 2023,
      ownerId: 'own-mahalaxmi', ownerName: 'Mahalaxmi Roadways', area: 'Hadapsar', city: 'Pune', distanceKm: 14.3,
      hourlyRate: 800, shiftRate: 6200, monthlyRate: 135000, mobilisationFee: 0, rating: 4.3, reviewCount: 64, hmr: 2410,
      utilisation: 0.77, reviews: _reviews,
      specs: {'Payload': '25 t', 'Body volume': '16 m³', 'Engine power': '280 HP', 'Wheel config': '10×4'},
      description: 'Rock body tipper for muck shifting and aggregate transport within 40 km.',
    ),
    Equipment(
      id: 'eq-008', name: 'JCB 530-110 Telehandler', make: 'JCB', model: '530-110', categoryId: 'telehandler', year: 2022,
      ownerId: 'own-patil', ownerName: ownerCompany, area: 'Baner', city: 'Pune', distanceKm: 7.5,
      hourlyRate: 1250, shiftRate: 9800, monthlyRate: 215000, mobilisationFee: 3500, rating: 4.7, reviewCount: 18, hmr: 1290,
      status: EquipmentStatus.maintenance, utilisation: 0.44, registration: 'MH 12 TM 3302', monthEarnings: 64000, reviews: _reviews,
      specs: {'Max capacity': '3,000 kg', 'Max lift height': '10.6 m', 'Forward reach': '7.2 m', 'Engine power': '109 HP'},
      description: 'Telehandler with fork and bucket attachments, suited to high-rise material handling.',
    ),
    Equipment(
      id: 'eq-009', name: 'Komatsu PC210 Excavator', make: 'Komatsu', model: 'PC210-10M0', categoryId: 'excavator', year: 2023,
      ownerId: 'own-deccan', ownerName: 'Deccan Earthworks', area: 'Hinjewadi', city: 'Pune', distanceKm: 5.1,
      hourlyRate: 2600, shiftRate: 20500, monthlyRate: 470000, mobilisationFee: 9500, rating: 4.9, reviewCount: 22, hmr: 1510,
      instantBook: true, utilisation: 0.71, reviews: _reviews,
      specs: {'Operating weight': '21,600 kg', 'Engine power': '163 HP', 'Bucket capacity': '1.05 m³', 'Max dig depth': '6.62 m', 'Reach': '9.88 m'},
      description: '2023 Komatsu with KOMTRAX telematics. Fuel-efficient and low noise for city sites.',
    ),
    Equipment(
      id: 'eq-010', name: 'Mahindra EarthMaster SX Backhoe', make: 'Mahindra', model: 'EarthMaster SX', categoryId: 'backhoe', year: 2021,
      ownerId: 'own-deccan', ownerName: 'Deccan Earthworks', area: 'Pimpri', city: 'Pune', distanceKm: 9.6,
      hourlyRate: 1000, shiftRate: 7800, monthlyRate: 170000, mobilisationFee: 2000, rating: 4.4, reviewCount: 71, hmr: 5230,
      utilisation: 0.66, reviews: _reviews,
      specs: {'Operating weight': '7,400 kg', 'Engine power': '74 HP', 'Bucket capacity': '1.0 m³', 'Dig depth': '4.5 m', 'Fuel tank': '115 L'},
      description: 'Economical backhoe for trenching and site cleaning. Operator speaks Marathi and Hindi.',
    ),
    Equipment(
      id: 'eq-011', name: 'Escorts F15 Pick & Carry Crane', make: 'Escorts', model: 'F15', categoryId: 'crane', year: 2020,
      ownerId: 'own-sahyadri', ownerName: 'Sahyadri Equipment Hire', area: 'Bhosari', city: 'Pune', distanceKm: 16.8,
      hourlyRate: 1550, shiftRate: 12000, monthlyRate: 280000, mobilisationFee: 0, rating: 4.2, reviewCount: 37, hmr: 6950,
      verified: false, utilisation: 0.49, reviews: _reviews,
      specs: {'Max capacity': '15 t', 'Boom length': '16.9 m', 'Max lift height': '17.5 m', 'Engine power': '75 HP'},
      description: 'Pick-and-carry crane for factory and warehouse projects.',
    ),
    Equipment(
      id: 'eq-012', name: 'Volvo SD110 Soil Compactor', make: 'Volvo', model: 'SD110', categoryId: 'roller', year: 2022,
      ownerId: 'own-patil', ownerName: ownerCompany, area: 'Hadapsar', city: 'Pune', distanceKm: 13.2,
      hourlyRate: 1150, shiftRate: 9000, monthlyRate: 190000, mobilisationFee: 6500, rating: 4.6, reviewCount: 26, hmr: 2780,
      status: EquipmentStatus.paused, utilisation: 0.38, registration: 'MH 12 VR 7765', monthEarnings: 42000, reviews: _reviews,
      specs: {'Operating weight': '11,200 kg', 'Drum width': '2,130 mm', 'Centrifugal force': '246 kN', 'Engine power': '105 HP'},
      description: 'Smooth drum compactor with padfoot shell kit available.',
    ),
  ];

  static Equipment equipmentById(String id) => equipment.firstWhere((e) => e.id == id, orElse: () => equipment.first);
  static List<Equipment> get ownerEquipment => equipment.where((e) => e.ownerId == 'own-patil').toList();

  // ---- Bookings ------------------------------------------------------------
  static final customerBookings = <Booking>[
    Booking(id: 'bk-101', reference: 'ER-2611-4X7K', equipmentId: 'eq-001', equipmentName: 'JCB 3DX Super Backhoe Loader', categoryId: 'backhoe',
        customerName: customerName, customerCompany: customerCompany, ownerName: ownerCompany, siteName: 'Wakad Residency – Plot 14',
        siteAddress: 'Survey 112, Bhumkar Chowk, Wakad, Pune 411057', start: day(0), end: day(2, 18), shiftLabel: '8 hr day shift',
        total: 30450, paid: 15225, status: BookingStatus.enRoute, operatorName: operatorName, etaMinutes: 18, hoursLogged: 0),
    Booking(id: 'bk-102', reference: 'ER-2611-2M9P', equipmentId: 'eq-009', equipmentName: 'Komatsu PC210 Excavator', categoryId: 'excavator',
        customerName: customerName, customerCompany: customerCompany, ownerName: 'Deccan Earthworks', siteName: 'Hinjewadi Phase 3 – Basement',
        siteAddress: 'Plot 7, Maan Road, Hinjewadi Phase 3, Pune 411057', start: day(-2), end: day(4, 18), shiftLabel: '8 hr day shift',
        total: 182900, paid: 91450, status: BookingStatus.active, operatorName: 'Sandeep More', hoursLogged: 21.5),
    Booking(id: 'bk-103', reference: 'ER-2611-8Q2D', equipmentId: 'eq-004', equipmentName: 'ACE 14XW Pick & Carry Crane', categoryId: 'crane',
        customerName: customerName, customerCompany: customerCompany, ownerName: ownerCompany, siteName: 'Wakad Residency – Plot 14',
        siteAddress: 'Survey 112, Bhumkar Chowk, Wakad, Pune 411057', start: day(5), end: day(6, 18), shiftLabel: '8 hr day shift',
        total: 32480, paid: 0, status: BookingStatus.requested),
    Booking(id: 'bk-104', reference: 'ER-2611-5T1R', equipmentId: 'eq-006', equipmentName: 'Schwing Stetter AM 8 Transit Mixer', categoryId: 'mixer',
        customerName: customerName, customerCompany: customerCompany, ownerName: 'Sahyadri Equipment Hire', siteName: 'Baner Commercial – Slab L4',
        siteAddress: 'Baner–Pashan Link Road, Baner, Pune 411045', start: day(3), end: day(3, 18), shiftLabel: '8 hr day shift',
        total: 9440, paid: 9440, status: BookingStatus.confirmed),
    Booking(id: 'bk-105', reference: 'ER-2610-9H3C', equipmentId: 'eq-007', equipmentName: 'BharatBenz 2828C Tipper', categoryId: 'tipper',
        customerName: customerName, customerCompany: customerCompany, ownerName: 'Mahalaxmi Roadways', siteName: 'Hinjewadi Phase 3 – Basement',
        siteAddress: 'Plot 7, Maan Road, Hinjewadi Phase 3, Pune 411057', start: day(-14), end: day(-10, 18), shiftLabel: '8 hr day shift',
        total: 36580, paid: 36580, status: BookingStatus.completed, operatorName: 'Rahul Pawar', hoursLogged: 39),
    Booking(id: 'bk-106', reference: 'ER-2610-3K8L', equipmentId: 'eq-005', equipmentName: 'Case 1107EX Soil Compactor', categoryId: 'roller',
        customerName: customerName, customerCompany: customerCompany, ownerName: 'Mahalaxmi Roadways', siteName: 'Talegaon Warehouse Road',
        siteAddress: 'MIDC Phase 2, Talegaon Dabhade, Pune 410507', start: day(-25), end: day(-22, 18), shiftLabel: '8 hr day shift',
        total: 45800, paid: 45800, status: BookingStatus.completed, operatorName: 'Ganesh Shinde', hoursLogged: 31),
  ];

  static final ownerBookings = <Booking>[
    Booking(id: 'ob-201', reference: 'ER-2611-8Q2D', equipmentId: 'eq-004', equipmentName: 'ACE 14XW Pick & Carry Crane', categoryId: 'crane',
        customerName: customerName, customerCompany: customerCompany, ownerName: ownerCompany, siteName: 'Wakad Residency – Plot 14',
        siteAddress: 'Wakad, Pune', start: day(5), end: day(6, 18), shiftLabel: '8 hr day shift', total: 32480, paid: 0,
        status: BookingStatus.requested, createdAt: DateTime(2026, 11, 12, 9, 10)),
    Booking(id: 'ob-202', reference: 'ER-2611-6W4N', equipmentId: 'eq-001', equipmentName: 'JCB 3DX Super Backhoe Loader', categoryId: 'backhoe',
        customerName: 'Prakash Jadhav', customerCompany: 'Jadhav Builders', ownerName: ownerCompany, siteName: 'Ravet Row Houses',
        siteAddress: 'Ravet, Pune', start: day(8), end: day(12, 18), shiftLabel: '8 hr day shift', total: 50750, paid: 0,
        status: BookingStatus.requested, createdAt: DateTime(2026, 11, 12, 7, 45)),
    Booking(id: 'ob-203', reference: 'ER-2611-4X7K', equipmentId: 'eq-001', equipmentName: 'JCB 3DX Super Backhoe Loader', categoryId: 'backhoe',
        customerName: customerName, customerCompany: customerCompany, ownerName: ownerCompany, siteName: 'Wakad Residency – Plot 14',
        siteAddress: 'Wakad, Pune', start: day(0), end: day(2, 18), shiftLabel: '8 hr day shift', total: 30450, paid: 15225,
        status: BookingStatus.enRoute, operatorName: operatorName),
    Booking(id: 'ob-204', reference: 'ER-2611-1B6V', equipmentId: 'eq-002', equipmentName: 'Tata Hitachi EX200 Excavator', categoryId: 'excavator',
        customerName: 'Sneha Kale', customerCompany: 'Kale Infra Projects', ownerName: ownerCompany, siteName: 'Punawale Township – Phase 2',
        siteAddress: 'Punawale, Pune', start: day(-6), end: day(9, 18), shiftLabel: 'Monthly (200 hrs)', total: 420000, paid: 210000,
        status: BookingStatus.active, operatorName: 'Vikas Gaikwad', hoursLogged: 46),
    Booking(id: 'ob-205', reference: 'ER-2611-7C2Z', equipmentId: 'eq-004', equipmentName: 'ACE 14XW Pick & Carry Crane', categoryId: 'crane',
        customerName: 'Amit Bhosale', customerCompany: 'Bhosale Roadlines', ownerName: ownerCompany, siteName: 'Chakan Logistics Park',
        siteAddress: 'Chakan, Pune', start: day(14), end: day(15, 18), shiftLabel: '8 hr day shift', total: 31860, paid: 15930,
        status: BookingStatus.confirmed),
    Booking(id: 'ob-206', reference: 'ER-2610-2D5F', equipmentId: 'eq-012', equipmentName: 'Volvo SD110 Soil Compactor', categoryId: 'roller',
        customerName: 'Prakash Jadhav', customerCompany: 'Jadhav Builders', ownerName: ownerCompany, siteName: 'Ravet Internal Roads',
        siteAddress: 'Ravet, Pune', start: day(-20), end: day(-16, 18), shiftLabel: '8 hr day shift', total: 53100, paid: 53100,
        status: BookingStatus.completed, operatorName: 'Ganesh Shinde', hoursLogged: 40),
  ];

  // ---- Payments --------------------------------------------------------------
  static final customerPayments = <PaymentRecord>[
    PaymentRecord(id: 'pay-1', bookingRef: 'ER-2611-4X7K', description: 'Advance · JCB 3DX Super', amount: 15225, method: 'UPI · ravi@okhdfcbank', date: DateTime(2026, 11, 11, 18, 4), status: PaymentStatus.success),
    PaymentRecord(id: 'pay-2', bookingRef: 'ER-2611-2M9P', description: 'Advance · Komatsu PC210', amount: 91450, method: 'NEFT · HDFC Bank', date: DateTime(2026, 11, 9, 11, 30), status: PaymentStatus.success),
    PaymentRecord(id: 'pay-3', bookingRef: 'ER-2611-5T1R', description: 'Full payment · Transit mixer', amount: 9440, method: 'Card · Visa •• 4021', date: DateTime(2026, 11, 10, 16, 12), status: PaymentStatus.success),
    PaymentRecord(id: 'pay-4', bookingRef: 'ER-2611-2M9P', description: 'Security deposit', amount: 25000, method: 'UPI · ravi@okhdfcbank', date: DateTime(2026, 11, 9, 11, 32), status: PaymentStatus.pending),
    PaymentRecord(id: 'pay-5', bookingRef: 'ER-2610-9H3C', description: 'Final bill · Tipper', amount: 18290, method: 'UPI · ravi@okhdfcbank', date: DateTime(2026, 11, 2, 9, 45), status: PaymentStatus.success),
    PaymentRecord(id: 'pay-6', bookingRef: 'ER-2610-9H3C', description: 'Deposit refund', amount: 10000, method: 'UPI · ravi@okhdfcbank', date: DateTime(2026, 11, 3, 14, 20), status: PaymentStatus.refunded),
    PaymentRecord(id: 'pay-7', bookingRef: 'ER-2610-3K8L', description: 'Final bill · Soil compactor', amount: 22900, method: 'Card · Visa •• 4021', date: DateTime(2026, 10, 22, 19, 5), status: PaymentStatus.failed),
  ];

  static final platformPayments = <PaymentRecord>[
    PaymentRecord(id: 'tx-9001', bookingRef: 'ER-2611-4X7K', description: 'Advance', amount: 15225, method: 'UPI', date: DateTime(2026, 11, 11, 18, 4), status: PaymentStatus.success, payer: customerCompany, payee: ownerCompany),
    PaymentRecord(id: 'tx-9002', bookingRef: 'ER-2611-1B6V', description: 'Monthly advance', amount: 210000, method: 'NEFT', date: DateTime(2026, 11, 6, 12, 2), status: PaymentStatus.success, payer: 'Kale Infra Projects', payee: ownerCompany),
    PaymentRecord(id: 'tx-9003', bookingRef: 'ER-2611-2M9P', description: 'Advance', amount: 91450, method: 'NEFT', date: DateTime(2026, 11, 9, 11, 30), status: PaymentStatus.success, payer: customerCompany, payee: 'Deccan Earthworks'),
    PaymentRecord(id: 'tx-9004', bookingRef: 'ER-2611-2M9P', description: 'Security deposit', amount: 25000, method: 'UPI', date: DateTime(2026, 11, 9, 11, 32), status: PaymentStatus.pending, payer: customerCompany, payee: 'Escrow'),
    PaymentRecord(id: 'tx-9005', bookingRef: 'ER-2611-7C2Z', description: 'Advance', amount: 15930, method: 'Card', date: DateTime(2026, 11, 8, 15, 40), status: PaymentStatus.success, payer: 'Bhosale Roadlines', payee: ownerCompany),
    PaymentRecord(id: 'tx-9006', bookingRef: 'ER-2610-3K8L', description: 'Final bill', amount: 22900, method: 'Card', date: DateTime(2026, 10, 22, 19, 5), status: PaymentStatus.failed, payer: customerCompany, payee: 'Mahalaxmi Roadways'),
    PaymentRecord(id: 'tx-9007', bookingRef: 'ER-2610-9H3C', description: 'Deposit refund', amount: 10000, method: 'UPI', date: DateTime(2026, 11, 3, 14, 20), status: PaymentStatus.refunded, payer: 'Escrow', payee: customerCompany),
    PaymentRecord(id: 'tx-9008', bookingRef: 'ER-2611-5T1R', description: 'Full payment', amount: 9440, method: 'Card', date: DateTime(2026, 11, 10, 16, 12), status: PaymentStatus.success, payer: customerCompany, payee: 'Sahyadri Equipment Hire'),
  ];

  // ---- Operators & jobs ----------------------------------------------------
  static final operators = <OperatorProfile>[
    OperatorProfile(id: 'op-1', name: operatorName, phone: '+91 98220 41178', experienceYears: 8, licence: 'MH12 2014 0045321', skills: ['Backhoe', 'Excavator'], rating: 4.8, status: OperatorStatus.onJob, hoursThisMonth: 86, currentAssignment: 'JCB 3DX · Wakad', licenceExpiry: DateTime(2028, 3, 14)),
    OperatorProfile(id: 'op-2', name: 'Vikas Gaikwad', phone: '+91 97654 22310', experienceYears: 11, licence: 'MH14 2011 0098812', skills: ['Excavator', 'Dozer'], rating: 4.9, status: OperatorStatus.onJob, hoursThisMonth: 104, currentAssignment: 'EX200 · Punawale', licenceExpiry: DateTime(2027, 1, 9)),
    OperatorProfile(id: 'op-3', name: 'Ganesh Shinde', phone: '+91 90110 87765', experienceYears: 6, licence: 'MH12 2017 0034490', skills: ['Roller', 'Backhoe'], rating: 4.6, status: OperatorStatus.available, hoursThisMonth: 52, licenceExpiry: DateTime(2026, 12, 2)),
    OperatorProfile(id: 'op-4', name: 'Rahul Pawar', phone: '+91 88050 11924', experienceYears: 4, licence: 'MH42 2019 0011873', skills: ['Tipper', 'Transit mixer'], rating: 4.4, status: OperatorStatus.available, hoursThisMonth: 61, licenceExpiry: DateTime(2029, 6, 30)),
    OperatorProfile(id: 'op-5', name: 'Mahesh Kamble', phone: '+91 99700 55432', experienceYears: 14, licence: 'MH12 2009 0076120', skills: ['Crane', 'Telehandler'], rating: 4.9, status: OperatorStatus.offDuty, hoursThisMonth: 73, licenceExpiry: DateTime(2027, 8, 21)),
    OperatorProfile(id: 'op-6', name: 'Sachin Waghmare', phone: '+91 95450 31207', experienceYears: 3, licence: 'MH14 2021 0066218', skills: ['Backhoe'], rating: 4.2, status: OperatorStatus.available, hoursThisMonth: 38, licenceExpiry: DateTime(2030, 2, 11)),
  ];

  static final operatorJobs = <OperatorJob>[
    OperatorJob(id: 'job-1', bookingRef: 'ER-2611-4X7K', equipmentName: 'JCB 3DX Super · MH 12 KX 4471', categoryId: 'backhoe', customer: customerCompany,
        siteName: 'Wakad Residency – Plot 14', siteAddress: 'Survey 112, Bhumkar Chowk, Wakad, Pune 411057', start: day(0), end: day(2, 18),
        shift: '8:00 AM – 6:00 PM', status: JobStatus.enRoute, contactName: 'Ravi Deshmukh (site)', contactPhone: '+91 98230 11456', distanceKm: 4.2,
        instructions: 'Enter from the service road behind Bhumkar Chowk. Foundation trenching for Block B; site engineer will mark lines. Fuel will be provided on site.', startHmr: 3460),
    OperatorJob(id: 'job-2', bookingRef: 'ER-2611-9J4S', equipmentName: 'JCB 3DX Super · MH 12 KX 4471', categoryId: 'backhoe', customer: 'Jadhav Builders',
        siteName: 'Ravet Row Houses', siteAddress: 'Sector 32, Ravet, Pune 412101', start: day(3), end: day(4, 18), shift: '8:00 AM – 6:00 PM',
        status: JobStatus.assigned, contactName: 'Prakash Jadhav', contactPhone: '+91 98500 77231', distanceKm: 11.6,
        instructions: 'Drainage trench along the internal road, approx. 120 m. Bring the 450 mm bucket.'),
    OperatorJob(id: 'job-3', bookingRef: 'ER-2611-3P8E', equipmentName: 'Mahindra EarthMaster SX', categoryId: 'backhoe', customer: 'Kale Infra Projects',
        siteName: 'Punawale Township – Phase 2', siteAddress: 'Punawale, Pune 411033', start: day(6), end: day(6, 18), shift: '8:00 AM – 6:00 PM',
        status: JobStatus.accepted, contactName: 'Sneha Kale', contactPhone: '+91 97300 44120', distanceKm: 9.3,
        instructions: 'Site cleaning and loading debris into tippers.'),
    OperatorJob(id: 'job-4', bookingRef: 'ER-2610-7N2A', equipmentName: 'JCB 3DX Super · MH 12 KX 4471', categoryId: 'backhoe', customer: 'Bhosale Roadlines',
        siteName: 'Chakan Logistics Park', siteAddress: 'Chakan MIDC, Pune 410501', start: day(-9), end: day(-7, 18), shift: '8:00 AM – 6:00 PM',
        status: JobStatus.completed, contactName: 'Amit Bhosale', contactPhone: '+91 90280 66112', distanceKm: 22.4,
        instructions: '', hoursLogged: 25.5, startHmr: 3409, rating: 5),
    OperatorJob(id: 'job-5', bookingRef: 'ER-2610-5V9K', equipmentName: 'JCB 3DX Super · MH 12 KX 4471', categoryId: 'backhoe', customer: customerCompany,
        siteName: 'Hinjewadi Phase 3 – Basement', siteAddress: 'Maan Road, Hinjewadi, Pune 411057', start: day(-16), end: day(-13, 18), shift: '8:00 AM – 6:00 PM',
        status: JobStatus.completed, contactName: 'Ravi Deshmukh', contactPhone: '+91 98230 11456', distanceKm: 5.8,
        instructions: '', hoursLogged: 33, startHmr: 3376, rating: 4.5),
    OperatorJob(id: 'job-6', bookingRef: 'ER-2610-1Q6M', equipmentName: 'Mahindra EarthMaster SX', categoryId: 'backhoe', customer: 'Jadhav Builders',
        siteName: 'Ravet Internal Roads', siteAddress: 'Ravet, Pune 412101', start: day(-24), end: day(-22, 18), shift: '8:00 AM – 6:00 PM',
        status: JobStatus.completed, contactName: 'Prakash Jadhav', contactPhone: '+91 98500 77231', distanceKm: 11.6,
        instructions: '', hoursLogged: 24, startHmr: 5190, rating: 4.8),
  ];

  // ---- Maintenance ---------------------------------------------------------
  static final maintenance = <MaintenanceTask>[
    MaintenanceTask(id: 'm-1', equipmentName: 'JCB 530-110 Telehandler', task: 'Hydraulic hose replacement', dueHmr: 1290, currentHmr: 1290, dueDate: day(0), status: MaintenanceStatus.overdue, cost: 18500, vendor: 'JCB Dealer – Bhosari'),
    MaintenanceTask(id: 'm-2', equipmentName: 'JCB 3DX Super Backhoe Loader', task: '500-hour service (oil, filters)', dueHmr: 3500, currentHmr: 3460, dueDate: day(4), status: MaintenanceStatus.dueSoon, cost: 14200, vendor: 'JCB Dealer – Bhosari'),
    MaintenanceTask(id: 'm-3', equipmentName: 'Tata Hitachi EX200 Excavator', task: 'Track tension & undercarriage check', dueHmr: 6250, currentHmr: 6120, dueDate: day(11), status: MaintenanceStatus.scheduled, cost: 6500, vendor: 'In-house'),
    MaintenanceTask(id: 'm-4', equipmentName: 'ACE 14XW Pick & Carry Crane', task: 'Annual load test certificate', dueHmr: 2000, currentHmr: 1880, dueDate: day(19), status: MaintenanceStatus.scheduled, cost: 9000, vendor: 'Certified inspector'),
    MaintenanceTask(id: 'm-5', equipmentName: 'Volvo SD110 Soil Compactor', task: 'Drum bearing replacement', dueHmr: 2780, currentHmr: 2780, dueDate: day(-3), status: MaintenanceStatus.done, cost: 22800, vendor: 'Volvo CE – Chakan'),
    MaintenanceTask(id: 'm-6', equipmentName: 'Tata Hitachi EX200 Excavator', task: '1000-hour service', dueHmr: 6000, currentHmr: 6012, dueDate: day(-18), status: MaintenanceStatus.done, cost: 31500, vendor: 'Tata Hitachi – Pune'),
  ];

  // ---- Chat ----------------------------------------------------------------
  static final chatThreads = <ChatThread>[
    ChatThread(id: 'c-1', name: 'Patil Earthmovers', context: 'JCB 3DX · ER-2611-4X7K', unread: 2, online: true, messages: [
      ChatMessage(text: 'Hello, booking confirmed for today. Operator Imran will start from Hinjewadi yard at 8:15.', fromMe: false, time: DateTime(2026, 11, 12, 7, 52)),
      ChatMessage(text: 'Thanks. Please ask him to come via the service road, main gate is blocked for slab work.', fromMe: true, time: DateTime(2026, 11, 12, 7, 58)),
      ChatMessage(text: 'Noted 👍 Will inform him.', fromMe: false, time: DateTime(2026, 11, 12, 8, 1)),
      ChatMessage(text: 'He is on the way, ETA about 20 minutes.', fromMe: false, time: DateTime(2026, 11, 12, 10, 12)),
    ]),
    ChatThread(id: 'c-2', name: 'Deccan Earthworks', context: 'Komatsu PC210 · ER-2611-2M9P', messages: [
      ChatMessage(text: 'Yesterday\'s hour log is 8.5 hrs. Please approve in the app.', fromMe: false, time: DateTime(2026, 11, 11, 19, 20)),
      ChatMessage(text: 'Approved. Can we extend by two more days?', fromMe: true, time: DateTime(2026, 11, 11, 20, 2)),
      ChatMessage(text: 'Yes, machine is free till 20th. Send extension request.', fromMe: false, time: DateTime(2026, 11, 11, 20, 15)),
    ]),
    ChatThread(id: 'c-3', name: 'EQUIPRENT Support', context: 'Help centre', online: true, messages: [
      ChatMessage(text: 'Hi Ravi, how can we help you today?', fromMe: false, time: DateTime(2026, 11, 8, 12, 0)),
    ]),
  ];

  // ---- Admin ---------------------------------------------------------------
  static final users = <PlatformUser>[
    PlatformUser(id: 'u-1', name: customerName, role: 'Customer', organisation: customerCompany, city: 'Pune', status: AccountStatus.active, kyc: KycStatus.verified, joined: DateTime(2026, 6, 3), bookings: 14),
    PlatformUser(id: 'u-2', name: ownerName, role: 'Owner', organisation: ownerCompany, city: 'Pune', status: AccountStatus.active, kyc: KycStatus.verified, joined: DateTime(2026, 5, 18), bookings: 62),
    PlatformUser(id: 'u-3', name: 'Farhan Qureshi', role: 'Owner', organisation: 'Qureshi Crane Services', city: 'Pune', status: AccountStatus.pending, kyc: KycStatus.pending, joined: DateTime(2026, 11, 10), bookings: 0),
    PlatformUser(id: 'u-4', name: 'Sneha Kale', role: 'Customer', organisation: 'Kale Infra Projects', city: 'Pune', status: AccountStatus.active, kyc: KycStatus.verified, joined: DateTime(2026, 7, 22), bookings: 9),
    PlatformUser(id: 'u-5', name: fleetManagerName, role: 'Fleet Manager', organisation: fleetCompany, city: 'Pune', status: AccountStatus.active, kyc: KycStatus.verified, joined: DateTime(2026, 6, 1), bookings: 0),
    PlatformUser(id: 'u-6', name: operatorName, role: 'Operator', organisation: ownerCompany, city: 'Pune', status: AccountStatus.active, kyc: KycStatus.verified, joined: DateTime(2026, 6, 9), bookings: 0),
    PlatformUser(id: 'u-7', name: 'Vijay Rathod', role: 'Customer', organisation: 'Rathod Developers', city: 'Pimpri-Chinchwad', status: AccountStatus.suspended, kyc: KycStatus.rejected, joined: DateTime(2026, 9, 14), bookings: 2),
    PlatformUser(id: 'u-8', name: 'Kiran Lokhande', role: 'Owner', organisation: 'Mahalaxmi Roadways', city: 'Pune', status: AccountStatus.active, kyc: KycStatus.verified, joined: DateTime(2026, 5, 30), bookings: 41),
    PlatformUser(id: 'u-9', name: 'Pooja Nair', role: 'Customer', organisation: 'Nair & Sons Contractors', city: 'Pune', status: AccountStatus.pending, kyc: KycStatus.pending, joined: DateTime(2026, 11, 11), bookings: 0),
  ];

  static final approvals = <ApprovalRequest>[
    ApprovalRequest(id: 'ap-1', type: 'KYC', title: 'Qureshi Crane Services — owner KYC', submittedBy: 'Farhan Qureshi', submittedAt: DateTime(2026, 11, 12, 8, 40),
        checks: {'PAN verified': true, 'GSTIN active': true, 'Bank name match': true, 'Address proof': false}, flag: 'Address on GST differs from bank'),
    ApprovalRequest(id: 'ap-2', type: 'Listing', title: 'ACE NX360 Crane — new listing', submittedBy: 'Farhan Qureshi', submittedAt: DateTime(2026, 11, 12, 9, 5),
        checks: {'RC matches owner': true, 'Insurance valid': true, 'Photos ≥ 4': true, 'Price within range': true}),
    ApprovalRequest(id: 'ap-3', type: 'KYC', title: 'Nair & Sons Contractors — customer KYC', submittedBy: 'Pooja Nair', submittedAt: DateTime(2026, 11, 11, 17, 22),
        checks: {'PAN verified': true, 'GSTIN active': true}),
    ApprovalRequest(id: 'ap-4', type: 'Bank change', title: 'Mahalaxmi Roadways — payout account change', submittedBy: 'Kiran Lokhande', submittedAt: DateTime(2026, 11, 11, 14, 3),
        checks: {'Penny drop': true, 'Name match': false, 'Step-up OTP': true}, flag: 'Account holder name mismatch'),
    ApprovalRequest(id: 'ap-5', type: 'Listing', title: 'JCB 3DX Plus — new listing', submittedBy: 'Deccan Earthworks', submittedAt: DateTime(2026, 11, 10, 11, 48),
        checks: {'RC matches owner': true, 'Insurance valid': false, 'Photos ≥ 4': true, 'Price within range': true}, flag: 'Insurance expires in 9 days'),
  ];

  static final disputes = <Dispute>[
    Dispute(id: 'dp-1', bookingRef: 'ER-2610-3K8L', raisedBy: customerCompany, against: 'Mahalaxmi Roadways', category: 'Hour meter (HMR)', amount: 4100, status: DisputeStatus.open, opened: DateTime(2026, 11, 10, 15, 0),
        summary: 'Customer says the machine was idle for 4 hours on 20 Oct due to breakdown, but the hours were billed.'),
    Dispute(id: 'dp-2', bookingRef: 'ER-2610-8R1T', raisedBy: 'Sahyadri Equipment Hire', against: 'Rathod Developers', category: 'Damage', amount: 38000, status: DisputeStatus.underReview, opened: DateTime(2026, 11, 7, 10, 30),
        summary: 'Owner reports a cracked bucket tooth adaptor at return. Handover photos show it intact.'),
    Dispute(id: 'dp-3', bookingRef: 'ER-2610-4G7H', raisedBy: 'Kale Infra Projects', against: 'Deccan Earthworks', category: 'Late arrival', amount: 6000, status: DisputeStatus.resolved, opened: DateTime(2026, 10, 28, 9, 15),
        summary: 'Machine arrived 5 hours late; customer requests mobilisation refund.', resolution: 'Mobilisation fee refunded to customer.'),
  ];

  static final settlements = <Settlement>[
    Settlement(id: 'st-1', ownerName: ownerCompany, bookings: 6, gross: 342600, commission: 34260, tds: 343, dueDate: day(1), status: SettlementStatus.pending),
    Settlement(id: 'st-2', ownerName: 'Deccan Earthworks', bookings: 4, gross: 228400, commission: 22840, tds: 228, dueDate: day(1), status: SettlementStatus.pending),
    Settlement(id: 'st-3', ownerName: 'Mahalaxmi Roadways', bookings: 3, gross: 96500, commission: 9650, tds: 97, dueDate: day(1), status: SettlementStatus.onHold),
    Settlement(id: 'st-4', ownerName: 'Sahyadri Equipment Hire', bookings: 5, gross: 187300, commission: 18730, tds: 187, dueDate: day(-1), status: SettlementStatus.released),
    Settlement(id: 'st-5', ownerName: ownerCompany, bookings: 7, gross: 401200, commission: 40120, tds: 401, dueDate: day(-8), status: SettlementStatus.released),
  ];

  // ---- Notifications ---------------------------------------------------------
  static final notifications = <AppNotification>[
    AppNotification(title: 'Machine on the way', body: 'JCB 3DX for Wakad Residency is 18 min away.', time: DateTime(2026, 11, 12, 10, 12), icon: AppIcons.navigation),
    AppNotification(title: 'Approve hour log', body: 'Komatsu PC210 logged 8.5 hrs on 11 Nov.', time: DateTime(2026, 11, 11, 19, 20), icon: AppIcons.clock),
    AppNotification(title: 'Payment received', body: '₹9,440 for transit mixer booking ER-2611-5T1R.', time: DateTime(2026, 11, 10, 16, 12), icon: AppIcons.checkCircle, read: true),
  ];

  // ---- Series for charts ---------------------------------------------------
  static const ownerMonthlyEarnings = [
    SeriesPoint('Jun', 412000), SeriesPoint('Jul', 386000), SeriesPoint('Aug', 298000),
    SeriesPoint('Sep', 455000), SeriesPoint('Oct', 521000), SeriesPoint('Nov', 347000),
  ];

  /// Gross booking value per month, in rupees.
  static const platformGmv = [
    SeriesPoint('Jun', 4200000), SeriesPoint('Jul', 5100000), SeriesPoint('Aug', 4600000),
    SeriesPoint('Sep', 6800000), SeriesPoint('Oct', 8900000), SeriesPoint('Nov', 7400000),
  ];

  static const bookingsByCategory = [
    SeriesPoint('Backhoe', 38), SeriesPoint('Excavator', 24), SeriesPoint('Tipper', 17),
    SeriesPoint('Crane', 11), SeriesPoint('Roller', 6), SeriesPoint('Other', 4),
  ];

  static const fleetUtilisation = [
    SeriesPoint('Mon', 0.72), SeriesPoint('Tue', 0.78), SeriesPoint('Wed', 0.81), SeriesPoint('Thu', 0.69),
    SeriesPoint('Fri', 0.84), SeriesPoint('Sat', 0.62), SeriesPoint('Sun', 0.31),
  ];
}
