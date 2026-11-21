import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../theme/app_icons.dart';
import '../utils/validators.dart';
import '../theme/app_tokens.dart';

/// Labelled text input. The label sits above the field (better for long
/// Indian-language labels than floating labels).
class AppTextField extends StatelessWidget {
  const AppTextField({
    super.key,
    required this.label,
    this.controller,
    this.initialValue,
    this.hint,
    this.helper,
    this.prefixIcon,
    this.prefixText,
    this.suffix,
    this.keyboardType,
    this.textInputAction,
    this.inputFormatters,
    this.validator,
    this.onChanged,
    this.onSubmitted,
    this.obscureText = false,
    this.enabled = true,
    this.readOnly = false,
    this.onTap,
    this.maxLines = 1,
    this.maxLength,
    this.requiredField = false,
  });

  final String label;
  final TextEditingController? controller;
  final String? initialValue;
  final String? hint;
  final String? helper;
  final IconData? prefixIcon;
  final String? prefixText;
  final Widget? suffix;
  final TextInputType? keyboardType;
  final TextInputAction? textInputAction;
  final List<TextInputFormatter>? inputFormatters;
  final FormFieldValidator<String>? validator;
  final ValueChanged<String>? onChanged;
  final ValueChanged<String>? onSubmitted;
  final bool obscureText;
  final bool enabled;
  final bool readOnly;
  final VoidCallback? onTap;
  final int maxLines;
  final int? maxLength;
  final bool requiredField;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text.rich(
          TextSpan(text: label, children: [
            if (requiredField) const TextSpan(text: ' *', style: TextStyle(color: AppColors.red600)),
          ]),
          style: theme.textTheme.labelLarge?.copyWith(color: AppColors.slate700),
        ),
        const SizedBox(height: AppSpacing.xs + 2),
        TextFormField(
          controller: controller,
          initialValue: controller == null ? initialValue : null,
          decoration: InputDecoration(
            hintText: hint,
            helperText: helper,
            helperMaxLines: 2,
            prefixIcon: prefixIcon != null ? Icon(prefixIcon, size: 18) : null,
            prefixText: prefixText,
            suffixIcon: suffix,
          ),
          keyboardType: keyboardType,
          textInputAction: textInputAction,
          inputFormatters: inputFormatters,
          validator: validator,
          onChanged: onChanged,
          onFieldSubmitted: onSubmitted,
          obscureText: obscureText,
          enabled: enabled,
          readOnly: readOnly,
          onTap: onTap,
          maxLines: obscureText ? 1 : maxLines,
          maxLength: maxLength,
          style: theme.textTheme.bodyLarge?.copyWith(fontSize: 15),
        ),
      ],
    );
  }
}

/// Rounded search input with optional trailing filter action.
class AppSearchField extends StatelessWidget {
  const AppSearchField({
    super.key,
    this.controller,
    this.hint = 'Search equipment, e.g. "20 ton excavator"',
    this.onChanged,
    this.onSubmitted,
    this.onTap,
    this.readOnly = false,
    this.trailing,
    this.autofocus = false,
  });

  final TextEditingController? controller;
  final String hint;
  final ValueChanged<String>? onChanged;
  final ValueChanged<String>? onSubmitted;
  final VoidCallback? onTap;
  final bool readOnly;
  final Widget? trailing;
  final bool autofocus;

  @override
  Widget build(BuildContext context) {
    return TextField(
      controller: controller,
      onChanged: onChanged,
      onSubmitted: onSubmitted,
      onTap: onTap,
      readOnly: readOnly,
      autofocus: autofocus,
      textInputAction: TextInputAction.search,
      decoration: InputDecoration(
        hintText: hint,
        prefixIcon: const Icon(AppIcons.search, size: 18),
        suffixIcon: trailing,
        border: const OutlineInputBorder(borderRadius: BorderRadius.all(Radius.circular(AppRadius.md)), borderSide: BorderSide(color: AppColors.slate300)),
        enabledBorder: const OutlineInputBorder(borderRadius: BorderRadius.all(Radius.circular(AppRadius.md)), borderSide: BorderSide(color: AppColors.slate300)),
      ),
    );
  }
}

/// Labelled dropdown.
class AppDropdown<T> extends StatelessWidget {
  const AppDropdown({
    super.key,
    required this.label,
    required this.value,
    required this.items,
    required this.onChanged,
    this.itemLabel,
    this.hint,
  });

  final String label;
  final T? value;
  final List<T> items;
  final ValueChanged<T?> onChanged;
  final String Function(T)? itemLabel;
  final String? hint;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(label, style: Theme.of(context).textTheme.labelLarge?.copyWith(color: AppColors.slate700)),
        const SizedBox(height: AppSpacing.xs + 2),
        DropdownButtonFormField<T>(
          initialValue: value,
          isExpanded: true,
          hint: hint == null ? null : Text(hint!),
          icon: const Icon(AppIcons.chevronDown, size: 18),
          borderRadius: AppRadius.control,
          items: [
            for (final item in items)
              DropdownMenuItem(value: item, child: Text(itemLabel?.call(item) ?? '$item', overflow: TextOverflow.ellipsis)),
          ],
          onChanged: onChanged,
        ),
      ],
    );
  }
}

/// Single-select chip group (filters, shift selection, segmented choices).
class AppChoiceChips<T> extends StatelessWidget {
  const AppChoiceChips({
    super.key,
    required this.options,
    required this.selected,
    required this.onSelected,
    required this.labelOf,
    this.iconOf,
  });

  final List<T> options;
  final T? selected;
  final ValueChanged<T> onSelected;
  final String Function(T) labelOf;
  final IconData? Function(T)? iconOf;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: AppSpacing.sm,
      runSpacing: AppSpacing.sm,
      children: [
        for (final o in options)
          ChoiceChip(
            label: Text(labelOf(o)),
            avatar: iconOf?.call(o) == null ? null : Icon(iconOf!(o), size: 16),
            selected: o == selected,
            showCheckmark: false,
            selectedColor: AppColors.navy700,
            labelStyle: Theme.of(context).textTheme.labelLarge?.copyWith(
                  color: o == selected ? AppColors.white : AppColors.slate700,
                ),
            side: BorderSide(color: o == selected ? AppColors.navy700 : AppColors.slate300),
            onSelected: (_) => onSelected(o),
          ),
      ],
    );
  }
}

/// − value + stepper for quantities (days, hours).
class QuantityStepper extends StatelessWidget {
  const QuantityStepper({super.key, required this.value, required this.onChanged, this.min = 1, this.max = 99, this.unit = ''});

  final int value;
  final ValueChanged<int> onChanged;
  final int min;
  final int max;
  final String unit;

  @override
  Widget build(BuildContext context) {
    Widget btn(IconData icon, String tip, VoidCallback? onTap) => IconButton(
          tooltip: tip,
          onPressed: onTap,
          icon: Icon(icon, size: 18),
          style: IconButton.styleFrom(
            side: const BorderSide(color: AppColors.slate300),
            shape: const RoundedRectangleBorder(borderRadius: AppRadius.control),
          ),
        );
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        btn(Icons.remove, 'Decrease', value > min ? () => onChanged(value - 1) : null),
        SizedBox(
          width: 72,
          child: Text(
            '$value${unit.isEmpty ? '' : ' $unit'}',
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.titleMedium,
          ),
        ),
        btn(Icons.add, 'Increase', value < max ? () => onChanged(value + 1) : null),
      ],
    );
  }
}

/// A tappable field that looks like an input and opens a picker.
class PickerField extends StatelessWidget {
  const PickerField({super.key, required this.label, required this.value, required this.icon, required this.onTap, this.placeholder = 'Select'});

  final String label;
  final String? value;
  final IconData icon;
  final VoidCallback onTap;
  final String placeholder;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(label, style: theme.textTheme.labelLarge?.copyWith(color: AppColors.slate700)),
        const SizedBox(height: AppSpacing.xs + 2),
        Material(
          color: AppColors.white,
          shape: const RoundedRectangleBorder(borderRadius: AppRadius.control, side: BorderSide(color: AppColors.slate300)),
          child: InkWell(
            borderRadius: AppRadius.control,
            onTap: onTap,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: AppSpacing.md, vertical: 14),
              child: Row(
                children: [
                  Icon(icon, size: 18, color: AppColors.slate500),
                  const SizedBox(width: AppSpacing.ms),
                  Expanded(
                    child: Text(
                      value ?? placeholder,
                      style: theme.textTheme.bodyLarge?.copyWith(fontSize: 15, color: value == null ? AppColors.slate400 : null),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  const Icon(AppIcons.chevronDown, size: 18, color: AppColors.slate500),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }
}

/// Date input that opens the calendar instead of accepting typed text. The controller holds the
/// date as YYYY-MM-DD (what the API expects) and stays empty until a date is picked.
class AppDateField extends StatelessWidget {
  const AppDateField({
    super.key,
    required this.label,
    required this.controller,
    this.hint = 'Tap to choose a date',
    this.requiredField = false,
    this.allowPast = true,
    this.firstDate,
    this.lastDate,
    this.onChanged,
  });

  final String label;
  final TextEditingController controller;
  final String hint;
  final bool requiredField;

  /// When false only today and later can be picked (licences, insurance, documents in force).
  final bool allowPast;
  final DateTime? firstDate;
  final DateTime? lastDate;
  final ValueChanged<DateTime>? onChanged;

  static String format(DateTime d) =>
      '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

  @override
  Widget build(BuildContext context) {
    final today = DateTime.now();
    final startOfToday = DateTime(today.year, today.month, today.day);
    return AppTextField(
      label: label,
      controller: controller,
      hint: hint,
      readOnly: true,
      requiredField: requiredField,
      prefixIcon: AppIcons.calendar,
      validator: (v) => Validators.isoDate(v, required: requiredField, future: !allowPast),
      onTap: () async {
        final current = DateTime.tryParse(controller.text.trim());
        final first = firstDate ?? (allowPast ? DateTime(today.year - 60) : startOfToday);
        final last = lastDate ?? DateTime(today.year + 30);
        var initial = current ?? (allowPast ? startOfToday : startOfToday);
        if (initial.isBefore(first)) initial = first;
        if (initial.isAfter(last)) initial = last;
        final picked = await showDatePicker(context: context, initialDate: initial, firstDate: first, lastDate: last);
        if (picked != null) {
          controller.text = format(picked);
          onChanged?.call(picked);
        }
      },
    );
  }
}
