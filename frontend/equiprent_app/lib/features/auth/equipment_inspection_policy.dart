import 'package:flutter/material.dart';


const equipmentInspectionPolicyVersion = '2026-11-18-v1';

String equipmentInspectionPolicy(Locale locale) {
  final hindi = locale.languageCode == 'hi';
  final marathi = locale.languageCode == 'mr';
  if (hindi) {
    return '''EQUIPRENT उपकरण निरीक्षण नीति

1. डिलीवरी से पहले और काम पूरा होने के बाद उपकरण की स्थिति के फोटो लेना आवश्यक है।
2. निरीक्षण के फोटो EQUIPRENT ऐप के लाइव कैमरे से सीधे लेने होंगे।
3. डिवाइस की गैलरी के पुराने फोटो निरीक्षण प्रमाण के रूप में स्वीकार नहीं किए जाएंगे।
4. उपकरण के आगे, पीछे, बाएं और दाएं हिस्से, महत्वपूर्ण भागों और मौजूदा क्षति के फोटो लें।
5. उपकरण मालिक और नियुक्त ड्राइवर अपने आवश्यक निरीक्षण चरण पूरे करेंगे।
6. ग्राहक उपकरण की स्थिति की समीक्षा करेंगे और दिखाई देने वाली क्षति या असहमति की रिपोर्ट करेंगे।
7. काम पूरा होने के बाद और वापसी की पुष्टि से पहले दूसरा निरीक्षण आवश्यक है।
8. उपयोगकर्ताओं को सही जानकारी देनी होगी और क्षति छिपानी या गलत बतानी नहीं चाहिए।
9. फोटो का उपयोग क्षति शिकायतों, बुकिंग विवादों और भुगतान विवादों की जांच में किया जा सकता है।
10. EQUIPRENT प्रशासक शिकायतों के समाधान के लिए संबंधित निरीक्षण रिकॉर्ड देख सकते हैं।
11. केवल फोटो के आधार पर कोई जुर्माना या क्षति शुल्क स्वतः नहीं लगाया जाएगा। प्रशासक जांच करके लागू प्लेटफ़ॉर्म नीति के अनुसार कारण सहित निर्णय देंगे।
12. उचित अनुमति के बिना असंबंधित निजी जानकारी या लोगों की तस्वीरें न लें।

नीति संस्करण: $equipmentInspectionPolicyVersion''';
  }
  if (marathi) {
    return '''EQUIPRENT उपकरण तपासणी धोरण

1. उपकरण पोहोचवण्यापूर्वी आणि काम पूर्ण झाल्यानंतर त्याच्या स्थितीचे फोटो घेणे आवश्यक आहे.
2. तपासणीचे फोटो EQUIPRENT अॅपमधील लाइव्ह कॅमेऱ्याने थेट घ्यावेत.
3. डिव्हाइसच्या गॅलरीतील जुने फोटो तपासणीचा पुरावा म्हणून स्वीकारले जाणार नाहीत.
4. उपकरणाच्या पुढील, मागील, डाव्या आणि उजव्या बाजूंचे, महत्त्वाच्या भागांचे आणि आधीपासूनच्या नुकसानीचे फोटो घ्या.
5. उपकरणमालक आणि नियुक्त चालकांनी त्यांचे आवश्यक तपासणी टप्पे पूर्ण करावेत.
6. ग्राहकांनी उपकरणाची स्थिती तपासून दिसणारे नुकसान किंवा मतभेद कळवावेत.
7. काम पूर्ण झाल्यानंतर आणि परतावा निश्चित करण्यापूर्वी दुसरी तपासणी आवश्यक आहे.
8. वापरकर्त्यांनी अचूक माहिती द्यावी आणि नुकसान जाणीवपूर्वक लपवू किंवा चुकीचे दाखवू नये.
9. नुकसान तक्रारी, बुकिंग वाद आणि पेमेंट वाद तपासण्यासाठी फोटो वापरले जाऊ शकतात.
10. तक्रारी सोडवण्यासाठी EQUIPRENT प्रशासक संबंधित तपासणी नोंदी पाहू शकतात.
11. केवळ फोटोंच्या आधारे दंड किंवा नुकसान शुल्क आपोआप आकारले जाणार नाही. प्रशासक तपास करून लागू प्लॅटफॉर्म धोरणानुसार कारणासहित निर्णय देतील.
12. योग्य परवानगीशिवाय असंबंधित खाजगी माहिती किंवा लोकांचे फोटो घेऊ नका.

धोरण आवृत्ती: $equipmentInspectionPolicyVersion''';
  }
  return '''EQUIPRENT EQUIPMENT INSPECTION POLICY

1. EQUIPRENT requires equipment condition photos before delivery and after work completion.
2. Inspection photos must be captured directly using the live camera inside the EQUIPRENT application.
3. Previously taken photos from the device gallery are not accepted as equipment inspection evidence.
4. Photograph the front, back, left and right sides, important components and any existing damage.
5. Equipment owners and assigned drivers must complete their required inspection steps.
6. Customers must review the equipment condition and report visible damage or disagreement.
7. A second inspection is required after work is completed and before equipment return is confirmed.
8. Users must provide accurate information and must not intentionally hide or misrepresent equipment damage.
9. Inspection photographs may be used to investigate damage complaints, booking disputes and payment-related disputes.
10. EQUIPRENT administrators may review relevant inspection records to resolve complaints.
11. No fine or damage charge will be imposed automatically based only on photographs. An administrator must investigate and provide a reasoned decision under the applicable platform policy.
12. Do not photograph unrelated private information or people without appropriate permission.

Policy version: $equipmentInspectionPolicyVersion''';
}

class EquipmentInspectionPolicyPage extends StatelessWidget {
  const EquipmentInspectionPolicyPage({super.key});

  @override
  Widget build(BuildContext context) {
    final locale = Localizations.localeOf(context);
    return Scaffold(
      appBar: AppBar(title: Text(switch (locale.languageCode) {
        'hi' => 'निरीक्षण नीति',
        'mr' => 'तपासणी धोरण',
        _ => 'Equipment inspection policy',
      })),
      body: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: SelectableText(
            equipmentInspectionPolicy(locale),
            style: Theme.of(context).textTheme.bodyLarge?.copyWith(height: 1.65),
          ),
        ),
      ),
    );
  }
}
