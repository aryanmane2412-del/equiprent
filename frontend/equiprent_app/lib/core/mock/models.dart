import 'package:flutter/widgets.dart';

/// UI models for the design-stage mock data. They mirror the planned API
/// resources (docs/API_DESIGN.md) closely enough to be swapped for DTOs later.

@immutable
class EquipmentCategory {
  const EquipmentCategory({
    required this.id,
    required this.name,
    required this.icon,
    required this.description,
    required this.available,
    required this.fromShiftRate,
  });

  final String id;
  final String name;
  final IconData icon;
  final String description;
  final int available;
  final int fromShiftRate;
}

enum EquipmentStatus { available, onRent, maintenance, paused }

@immutable
class Review {
  const Review({required this.author, required this.company, required this.rating, required this.text, required this.date});

  final String author;
  final String company;
  final double rating;
  final String text;
  final DateTime date;
}

@immutable
class Equipment {
  const Equipment({
    required this.id,
    required this.name,
    required this.make,
    required this.model,
    required this.categoryId,
    required this.year,
    required this.ownerId,
    required this.ownerName,
    required this.area,
    required this.city,
    required this.distanceKm,
    required this.hourlyRate,
    required this.shiftRate,
    required this.monthlyRate,
    required this.mobilisationFee,
    required this.rating,
    required this.reviewCount,
    required this.hmr,
    required this.specs,
    required this.description,
    this.verified = true,
    this.instantBook = false,
    this.withOperator = true,
    this.status = EquipmentStatus.available,
    this.utilisation = 0.0,
    this.reviews = const [],
    this.registration,
    this.monthEarnings = 0,
  });

  final String id;
  final String name;
  final String make;
  final String model;
  final String categoryId;
  final int year;
  final String ownerId;
  final String ownerName;
  final String area;
  final String city;
  final double distanceKm;
  final int hourlyRate;
  final int shiftRate; // 8-hour shift
  final int monthlyRate; // 200 hours included
  final int mobilisationFee;
  final double rating;
  final int reviewCount;
  final int hmr; // hour meter reading
  final Map<String, String> specs;
  final String description;
  final bool verified;
  final bool instantBook;
  final bool withOperator;
  final EquipmentStatus status;
  final double utilisation; // 0..1, last 30 days
  final List<Review> reviews;
  final String? registration;
  final int monthEarnings;

  String get location => '$area, $city';

  Equipment copyWith({EquipmentStatus? status}) => Equipment(
        id: id,
        name: name,
        make: make,
        model: model,
        categoryId: categoryId,
        year: year,
        ownerId: ownerId,
        ownerName: ownerName,
        area: area,
        city: city,
        distanceKm: distanceKm,
        hourlyRate: hourlyRate,
        shiftRate: shiftRate,
        monthlyRate: monthlyRate,
        mobilisationFee: mobilisationFee,
        rating: rating,
        reviewCount: reviewCount,
        hmr: hmr,
        specs: specs,
        description: description,
        verified: verified,
        instantBook: instantBook,
        withOperator: withOperator,
        status: status ?? this.status,
        utilisation: utilisation,
        reviews: reviews,
        registration: registration,
        monthEarnings: monthEarnings,
      );
}

enum BookingStatus { requested, confirmed, operatorAssigned, enRoute, active, completed, cancelled, disputed }

extension BookingStatusX on BookingStatus {
  String get label => switch (this) {
        BookingStatus.requested => 'Awaiting owner',
        BookingStatus.confirmed => 'Confirmed',
        BookingStatus.operatorAssigned => 'Operator assigned',
        BookingStatus.enRoute => 'On the way',
        BookingStatus.active => 'Working on site',
        BookingStatus.completed => 'Completed',
        BookingStatus.cancelled => 'Cancelled',
        BookingStatus.disputed => 'In dispute',
      };

  bool get isLive => this == BookingStatus.enRoute || this == BookingStatus.active;
  bool get isUpcoming =>
      this == BookingStatus.requested || this == BookingStatus.confirmed || this == BookingStatus.operatorAssigned;
}

@immutable
class Booking {
  const Booking({
    required this.id,
    required this.reference,
    required this.equipmentId,
    required this.equipmentName,
    required this.categoryId,
    required this.customerName,
    required this.customerCompany,
    required this.ownerName,
    required this.siteName,
    required this.siteAddress,
    required this.start,
    required this.end,
    required this.shiftLabel,
    required this.total,
    required this.paid,
    required this.status,
    this.operatorName,
    this.hoursLogged = 0,
    this.etaMinutes,
    this.createdAt,
  });

  final String id;
  final String reference;
  final String equipmentId;
  final String equipmentName;
  final String categoryId;
  final String customerName;
  final String customerCompany;
  final String ownerName;
  final String siteName;
  final String siteAddress;
  final DateTime start;
  final DateTime end;
  final String shiftLabel;
  final int total;
  final int paid;
  final BookingStatus status;
  final String? operatorName;
  final double hoursLogged;
  final int? etaMinutes;
  final DateTime? createdAt;

  int get balance => total - paid;
  int get days => end.difference(start).inDays + 1;

  Booking copyWith({BookingStatus? status, int? paid, String? operatorName}) => Booking(
        id: id,
        reference: reference,
        equipmentId: equipmentId,
        equipmentName: equipmentName,
        categoryId: categoryId,
        customerName: customerName,
        customerCompany: customerCompany,
        ownerName: ownerName,
        siteName: siteName,
        siteAddress: siteAddress,
        start: start,
        end: end,
        shiftLabel: shiftLabel,
        total: total,
        paid: paid ?? this.paid,
        status: status ?? this.status,
        operatorName: operatorName ?? this.operatorName,
        hoursLogged: hoursLogged,
        etaMinutes: etaMinutes,
        createdAt: createdAt,
      );
}

enum PaymentStatus { success, pending, refunded, failed }

@immutable
class PaymentRecord {
  const PaymentRecord({
    required this.id,
    required this.bookingRef,
    required this.description,
    required this.amount,
    required this.method,
    required this.date,
    required this.status,
    this.payer = '',
    this.payee = '',
  });

  final String id;
  final String bookingRef;
  final String description;
  final int amount;
  final String method;
  final DateTime date;
  final PaymentStatus status;
  final String payer;
  final String payee;
}

enum OperatorStatus { onJob, available, offDuty }

@immutable
class OperatorProfile {
  const OperatorProfile({
    required this.id,
    required this.name,
    required this.phone,
    required this.experienceYears,
    required this.licence,
    required this.skills,
    required this.rating,
    required this.status,
    required this.hoursThisMonth,
    this.currentAssignment,
    this.licenceExpiry,
  });

  final String id;
  final String name;
  final String phone;
  final int experienceYears;
  final String licence;
  final List<String> skills;
  final double rating;
  final OperatorStatus status;
  final double hoursThisMonth;
  final String? currentAssignment;
  final DateTime? licenceExpiry;
}

enum JobStatus { assigned, accepted, enRoute, onSite, inProgress, completed }

extension JobStatusX on JobStatus {
  String get label => switch (this) {
        JobStatus.assigned => 'New assignment',
        JobStatus.accepted => 'Accepted',
        JobStatus.enRoute => 'On the way',
        JobStatus.onSite => 'Arrived on site',
        JobStatus.inProgress => 'Work in progress',
        JobStatus.completed => 'Completed',
      };
}

@immutable
class OperatorJob {
  const OperatorJob({
    required this.id,
    required this.bookingRef,
    required this.equipmentName,
    required this.categoryId,
    required this.customer,
    required this.siteName,
    required this.siteAddress,
    required this.start,
    required this.end,
    required this.shift,
    required this.status,
    required this.contactName,
    required this.contactPhone,
    required this.distanceKm,
    required this.instructions,
    this.hoursLogged = 0,
    this.startHmr,
    this.rating,
  });

  final String id;
  final String bookingRef;
  final String equipmentName;
  final String categoryId;
  final String customer;
  final String siteName;
  final String siteAddress;
  final DateTime start;
  final DateTime end;
  final String shift;
  final JobStatus status;
  final String contactName;
  final String contactPhone;
  final double distanceKm;
  final String instructions;
  final double hoursLogged;
  final int? startHmr;
  final double? rating;

  OperatorJob copyWith({JobStatus? status, int? startHmr, double? hoursLogged}) => OperatorJob(
        id: id,
        bookingRef: bookingRef,
        equipmentName: equipmentName,
        categoryId: categoryId,
        customer: customer,
        siteName: siteName,
        siteAddress: siteAddress,
        start: start,
        end: end,
        shift: shift,
        status: status ?? this.status,
        contactName: contactName,
        contactPhone: contactPhone,
        distanceKm: distanceKm,
        instructions: instructions,
        hoursLogged: hoursLogged ?? this.hoursLogged,
        startHmr: startHmr ?? this.startHmr,
        rating: rating,
      );
}

enum MaintenanceStatus { overdue, dueSoon, scheduled, done }

@immutable
class MaintenanceTask {
  const MaintenanceTask({
    required this.id,
    required this.equipmentName,
    required this.task,
    required this.dueHmr,
    required this.currentHmr,
    required this.dueDate,
    required this.status,
    this.cost,
    this.vendor,
  });

  final String id;
  final String equipmentName;
  final String task;
  final int dueHmr;
  final int currentHmr;
  final DateTime dueDate;
  final MaintenanceStatus status;
  final int? cost;
  final String? vendor;

  MaintenanceTask copyWith({MaintenanceStatus? status}) => MaintenanceTask(
        id: id,
        equipmentName: equipmentName,
        task: task,
        dueHmr: dueHmr,
        currentHmr: currentHmr,
        dueDate: dueDate,
        status: status ?? this.status,
        cost: cost,
        vendor: vendor,
      );
}

@immutable
class ChatMessage {
  const ChatMessage({required this.text, required this.fromMe, required this.time});

  final String text;
  final bool fromMe;
  final DateTime time;
}

@immutable
class ChatThread {
  const ChatThread({
    required this.id,
    required this.name,
    required this.context,
    required this.messages,
    this.unread = 0,
    this.online = false,
  });

  final String id;
  final String name;
  final String context;
  final List<ChatMessage> messages;
  final int unread;
  final bool online;

  ChatThread copyWith({List<ChatMessage>? messages, int? unread}) => ChatThread(
        id: id,
        name: name,
        context: context,
        messages: messages ?? this.messages,
        unread: unread ?? this.unread,
        online: online,
      );
}

enum AccountStatus { active, pending, suspended }

enum KycStatus { verified, pending, rejected }

@immutable
class PlatformUser {
  const PlatformUser({
    required this.id,
    required this.name,
    required this.role,
    required this.organisation,
    required this.city,
    required this.status,
    required this.kyc,
    required this.joined,
    required this.bookings,
  });

  final String id;
  final String name;
  final String role;
  final String organisation;
  final String city;
  final AccountStatus status;
  final KycStatus kyc;
  final DateTime joined;
  final int bookings;

  PlatformUser copyWith({AccountStatus? status}) => PlatformUser(
        id: id,
        name: name,
        role: role,
        organisation: organisation,
        city: city,
        status: status ?? this.status,
        kyc: kyc,
        joined: joined,
        bookings: bookings,
      );
}

enum ApprovalStatus { pending, approved, rejected }

@immutable
class ApprovalRequest {
  const ApprovalRequest({
    required this.id,
    required this.type,
    required this.title,
    required this.submittedBy,
    required this.submittedAt,
    required this.checks,
    this.status = ApprovalStatus.pending,
    this.flag,
  });

  final String id;
  final String type; // KYC, Listing, Bank change
  final String title;
  final String submittedBy;
  final DateTime submittedAt;
  final Map<String, bool> checks; // automated check → passed
  final ApprovalStatus status;
  final String? flag;

  ApprovalRequest copyWith({ApprovalStatus? status}) => ApprovalRequest(
        id: id,
        type: type,
        title: title,
        submittedBy: submittedBy,
        submittedAt: submittedAt,
        checks: checks,
        status: status ?? this.status,
        flag: flag,
      );
}

enum DisputeStatus { open, underReview, resolved }

@immutable
class Dispute {
  const Dispute({
    required this.id,
    required this.bookingRef,
    required this.raisedBy,
    required this.against,
    required this.category,
    required this.amount,
    required this.status,
    required this.opened,
    required this.summary,
    this.resolution,
  });

  final String id;
  final String bookingRef;
  final String raisedBy;
  final String against;
  final String category;
  final int amount;
  final DisputeStatus status;
  final DateTime opened;
  final String summary;
  final String? resolution;

  Dispute copyWith({DisputeStatus? status, String? resolution}) => Dispute(
        id: id,
        bookingRef: bookingRef,
        raisedBy: raisedBy,
        against: against,
        category: category,
        amount: amount,
        status: status ?? this.status,
        opened: opened,
        summary: summary,
        resolution: resolution ?? this.resolution,
      );
}

enum SettlementStatus { pending, onHold, released }

@immutable
class Settlement {
  const Settlement({
    required this.id,
    required this.ownerName,
    required this.bookings,
    required this.gross,
    required this.commission,
    required this.tds,
    required this.dueDate,
    required this.status,
  });

  final String id;
  final String ownerName;
  final int bookings;
  final int gross;
  final int commission;
  final int tds;
  final DateTime dueDate;
  final SettlementStatus status;

  int get net => gross - commission - tds;

  Settlement copyWith({SettlementStatus? status}) => Settlement(
        id: id,
        ownerName: ownerName,
        bookings: bookings,
        gross: gross,
        commission: commission,
        tds: tds,
        dueDate: dueDate,
        status: status ?? this.status,
      );
}

@immutable
class AppNotification {
  const AppNotification({required this.title, required this.body, required this.time, required this.icon, this.read = false});

  final String title;
  final String body;
  final DateTime time;
  final IconData icon;
  final bool read;
}

/// One point of a time series (label + value), for charts.
@immutable
class SeriesPoint {
  const SeriesPoint(this.label, this.value);

  final String label;
  final double value;
}
