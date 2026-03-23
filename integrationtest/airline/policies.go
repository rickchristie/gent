package airline

import "github.com/rickchristie/gent/policy"

// airlinePolicies returns 18 realistic airline policies for integration
// testing. The first 4 policies match the mock tool behavior in fixture.go.
// The remaining policies saturate the corpus, simulating a real-world
// airline knowledge base that the agent must search through.
func airlinePolicies() []*policy.Policy {
	return []*policy.Policy{
		// -----------------------------------------------------------------
		// Core policies (1-4): content matches fixture.go mock tool behavior
		// -----------------------------------------------------------------
		{
			Id: "flight-change-rebooking",
			FullContent: `## Flight Change and Rebooking

### Fee Structure

Changes made more than 24 hours before departure:
- Economy class: $50 per passenger per segment
- Business class: $25 per passenger per segment
- First class: no change fee

Changes made within 24 hours of departure:
- Economy class: $150 per passenger per segment
- Business class: $75 per passenger per segment
- First class: no change fee

### Fare Difference

If the new flight has a higher fare, the customer pays the difference
in addition to any change fee. If the new flight has a lower fare, the
difference is issued as a travel credit valid for 12 months.

### Frequent Flyer Exceptions

Gold members receive one complimentary change per booking regardless
of class. Platinum members have all change fees waived on every booking.

### Same-Day Confirmed Change

Passengers may change to an earlier or later flight on the same day
and same route for a $75 fee. Fare difference applies if the new flight
has a higher fare; no refund if cheaper.

Fee waivers:
- Gold and Platinum frequent flyer members: fee waived
- Business and First class passengers: fee waived

### Same-Day Standby

Same-day standby is available at no charge for all passengers. The
passenger keeps their original confirmed seat and is placed on the
standby list for the requested flight.

Same-day changes are not available for Basic Economy tickets.`,
			Keywords: []string{
				"change", "reschedule", "modify", "rebook", "fee",
				"same-day", "standby", "fare difference",
			},
			SyntheticQueries: []string{
				"customer wants to change their flight",
				"how much does it cost to reschedule",
				"rebooking fee for economy class",
				"can gold members change for free",
				"change to an earlier flight today",
				"can I fly standby on a later flight",
				"same day change fee",
			},
		},
		{
			Id: "cancellation-refund",
			FullContent: `## Cancellation and Refund Policy

### Refundable Tickets (Flex Fare)

Refundable tickets may be cancelled at any time before departure for a
full refund minus a $25 processing fee. Refunds are credited to the
original payment method within 7-10 business days for credit cards and
14-21 business days for bank transfers.

### Non-Refundable Tickets (Standard Fare)

Non-refundable tickets cancelled before departure receive a travel
credit equal to the ticket value minus a $100 cancellation fee. Travel
credits expire 12 months from the original booking date and can be
applied to any future booking.

### Basic Economy Tickets

Basic Economy tickets are non-refundable and non-changeable. The full
ticket value is forfeited upon cancellation. No travel credit is issued.

### 24-Hour Full Refund

All tickets booked directly with the airline may be cancelled within
24 hours of purchase for a full refund, regardless of ticket type
(including Basic Economy). The booking must have been made at least
7 days before departure.

### Airline-Initiated Cancellations

If the airline cancels the flight, the customer receives a full cash
refund regardless of ticket type. No processing fees apply. The refund
is processed automatically within 7 business days.`,
			Keywords: []string{
				"cancel", "refund", "cancellation", "refundable",
				"non-refundable", "travel credit", "basic economy",
			},
			SyntheticQueries: []string{
				"customer wants to cancel their booking",
				"how to get a refund for a cancelled flight",
				"what happens when I cancel a non-refundable ticket",
				"airline cancelled my flight",
			},
		},
		{
			Id: "baggage-allowance",
			FullContent: `## Baggage Allowance and Fees

### Carry-On Baggage

All passengers may bring one carry-on bag (max 22x14x9 inches) and one
personal item (purse, laptop bag, or small backpack) at no charge.

### Checked Baggage by Class

Economy class:
- First bag: $35 (included for frequent flyer Silver and above)
- Second bag: $45
- Third bag and beyond: $150 each

Business class:
- Two checked bags included (up to 70 lbs each)
- Additional bags: $75 each

First class:
- Three checked bags included (up to 70 lbs each)
- Additional bags: $75 each

### Overweight and Oversize Fees

- 51-70 lbs: $100 surcharge per bag
- 71-99 lbs: $200 surcharge per bag
- Over 100 lbs: not accepted
- Oversized (over 62 linear inches): $200 surcharge

### Special Items

Sports equipment, musical instruments, and pet carriers have separate
policies. Contact customer service for specific requirements and fees.`,
			Keywords: []string{
				"baggage", "luggage", "bag", "carry-on", "checked",
				"overweight", "oversize", "fee",
			},
			SyntheticQueries: []string{
				"how many bags can I bring",
				"baggage fee for economy",
				"overweight bag surcharge",
				"is carry-on free",
			},
		},
		{
			Id: "frequent-flyer-benefits",
			FullContent: `## Frequent Flyer Program Benefits

### Bronze Tier (10,000+ miles per year)

- Priority boarding (Zone 2)
- 10% bonus miles on all flights
- Dedicated customer service line

### Silver Tier (25,000+ miles per year)

- Priority boarding (Zone 1)
- 25% bonus miles on all flights
- Airport lounge access on international flights
- One free checked bag on all bookings
- Priority security screening at select airports

### Gold Tier (50,000+ miles per year)

- First to board (Zone 1 priority)
- 50% bonus miles on all flights
- Unlimited airport lounge access (domestic and international)
- Two free checked bags on all bookings
- One free flight change per booking
- One free cancellation per booking
- Complimentary seat selection including premium economy
- Priority rebooking during irregular operations

### Platinum Tier (100,000+ miles per year)

- All Gold benefits
- 100% bonus miles on all flights
- Three free checked bags on all bookings
- All change and cancellation fees waived
- Complimentary upgrades to next cabin when available (cleared
  24 hours before departure)
- Guaranteed seat on sold-out flights (within 24 hours of departure)`,
			Keywords: []string{
				"frequent flyer", "loyalty", "miles", "tier",
				"bronze", "silver", "gold", "platinum",
				"lounge", "upgrade", "bonus",
			},
			SyntheticQueries: []string{
				"what are the loyalty member benefits",
				"gold tier perks and privileges",
				"how many miles for platinum status",
				"do gold members get free bags",
			},
		},

		// -----------------------------------------------------------------
		// Additional realistic policies (5-13)
		// -----------------------------------------------------------------
		{
			Id: "delay-compensation",
			FullContent: `## Flight Delay and Cancellation Compensation

### DOT Refund Rules (Effective 2024)

If the airline cancels a flight or makes a significant schedule change
(3+ hours domestic, 6+ hours international), the passenger is entitled
to a full cash refund to the original payment method. This applies
regardless of ticket type. Refunds must be processed within 7 business
days for credit cards.

### Meal and Accommodation Vouchers

For delays of 2-4 hours: complimentary snack and beverage voucher
($15 value). For delays of 4-8 hours: meal voucher ($25 value). For
overnight delays: hotel accommodation at partner hotels plus meal
vouchers. Transportation between airport and hotel is provided.

### Rebooking on Alternative Flights

Passengers affected by delays or cancellations are rebooked on the
next available flight at no additional cost. If the next available
flight is on a partner airline, the rebooking is handled automatically.
Passengers may also request a refund instead of rebooking.

### No Cash Compensation in the US

Unlike EU261 regulations, US law does not require airlines to pay cash
compensation for delays. The airline provides meals, accommodation, and
rebooking as described above.`,
			Keywords: []string{
				"delay", "compensation", "cancelled", "voucher",
				"meal", "hotel", "DOT", "refund", "rebooking",
			},
			SyntheticQueries: []string{
				"flight is delayed what compensation do I get",
				"airline cancelled my flight what are my rights",
				"delayed over 3 hours what happens",
				"do I get a hotel for overnight delay",
			},
		},
		{
			Id: "24-hour-cancellation",
			FullContent: `## 24-Hour Risk-Free Cancellation

### DOT-Mandated 24-Hour Rule

All tickets booked directly with the airline may be cancelled within
24 hours of purchase for a full refund, regardless of ticket type
(including Basic Economy). This applies to all bookings made at least
7 days before the scheduled departure.

### Conditions

- The booking must have been made at least 7 days before departure
- The cancellation must be requested within 24 hours of the original
  booking time
- Applies to all fare classes including Basic Economy
- The refund is processed to the original payment method
- Processing time: 7-10 business days for credit cards

### How to Cancel

Customers can cancel within 24 hours through:
- The airline website (My Trips > Cancel Booking)
- The airline mobile app
- Calling customer service

### Important Note

This 24-hour window does NOT apply to bookings made through third-party
travel agencies or online travel agents. Customers must contact the
third party directly for their cancellation policy.`,
			Keywords: []string{
				"24 hour", "24-hour", "risk free", "free cancellation",
				"cancel within 24 hours", "DOT",
			},
			SyntheticQueries: []string{
				"I just booked can I cancel for free",
				"cancel within 24 hours of booking",
				"risk-free cancellation period",
				"bought ticket yesterday want to cancel",
			},
		},
		{
			Id: "involuntary-rebooking",
			FullContent: `## Involuntary Rebooking and Denied Boarding

### Oversold Flight Compensation

If a flight is oversold, the airline first seeks volunteers willing to
give up their seats in exchange for compensation (travel vouchers,
typically $200-$800 depending on route and demand).

### Involuntary Denied Boarding (IDB)

If insufficient volunteers come forward, passengers may be involuntarily
denied boarding. Under DOT rules, compensation is required:
- Arrival delay 0-1 hour: no compensation required
- Arrival delay 1-2 hours (domestic) or 1-4 hours (international):
  200% of one-way fare, max $775
- Arrival delay over 2 hours (domestic) or over 4 hours (international):
  400% of one-way fare, max $1,550

### Rebooking Priority

Denied passengers are rebooked on the next available flight at no cost.
If the airline cannot provide a seat within 2 hours, the passenger may
request a full refund instead. The airline covers meal and accommodation
costs during the wait.

### Who Is Protected

Passengers with confirmed reservations who checked in on time cannot be
denied boarding without compensation. Passengers who arrive late or do
not have confirmed seats are not covered.`,
			Keywords: []string{
				"denied boarding", "overbooked", "oversold", "bumped",
				"involuntary", "IDB", "compensation",
			},
			SyntheticQueries: []string{
				"flight is overbooked what are my rights",
				"denied boarding compensation amount",
				"bumped from flight",
				"overbooked and can't board",
			},
		},
		{
			Id: "unaccompanied-minor",
			FullContent: `## Unaccompanied Minor Policy

### Age Requirements

- Children under 5: must travel with an adult (18+)
- Children 5-7: unaccompanied minor service required on direct/nonstop
  flights only
- Children 8-14: unaccompanied minor service required on all flights
- Children 15-17: unaccompanied minor service optional

### Service Fee

The unaccompanied minor service fee is $150 per child per direction
(one-way). This covers escort through the airport, supervision during
connections, and handoff to the designated guardian at arrival.

### Required Documentation

- Completed Unaccompanied Minor form with contact details for both the
  sending and receiving guardians
- Valid photo ID for both guardians
- Gate pass for the sending guardian (provided at check-in)
- The receiving guardian must present photo ID matching the form at
  arrival

### Restrictions

- Not available on red-eye flights (departing after 9 PM)
- Not available on the last connecting flight of the day
- Maximum of one connection per itinerary
- The child must be checked in at the airport counter (online check-in
  not available)`,
			Keywords: []string{
				"unaccompanied", "minor", "child", "children",
				"kid", "young", "escort", "guardian",
			},
			SyntheticQueries: []string{
				"child traveling alone on a flight",
				"unaccompanied minor fee and requirements",
				"can my 10 year old fly alone",
				"what documents needed for child flying alone",
			},
		},
		{
			Id: "pet-travel",
			FullContent: `## Pet Travel Policy

### In-Cabin Pets

Small dogs, cats, and household birds may travel in the cabin in an
airline-approved carrier that fits under the seat. Fee: $125 per pet
per direction. Maximum one pet carrier per passenger. The combined
weight of pet and carrier must not exceed 20 lbs.

### Cargo Pets

Larger pets travel in the pressurized, temperature-controlled cargo
hold. Fee: $200 per pet per direction. Pets must be in an
IATA-compliant hard-sided kennel. Available on flights under 12 hours
with temperatures between 45-85F at origin, destination, and all
connection points.

### Breed Restrictions

Brachycephalic (snub-nosed) breeds are not accepted in cargo due to
respiratory risks. This includes Bulldogs, Pugs, Boston Terriers,
Pekingese, Shih Tzu, and similar breeds. These breeds may travel
in-cabin only if they meet the carrier size requirements.

### Service and Emotional Support Animals

Trained service dogs travel in the cabin at no charge. Passengers must
submit the DOT Service Animal Transportation Form at least 48 hours
before departure. Emotional support animals are not given special
accommodations and must follow standard pet travel policies.

### Health Documentation

A veterinary health certificate issued within 10 days of travel is
required for all pets. International travel may require additional
documentation including rabies vaccination records and import permits.`,
			Keywords: []string{
				"pet", "animal", "dog", "cat", "bird", "carrier",
				"cargo", "in-cabin", "service animal",
			},
			SyntheticQueries: []string{
				"flying with a dog or cat",
				"pet fee for in-cabin travel",
				"can I bring my pet on the plane",
				"service animal requirements",
				"breed restrictions for flying",
			},
		},
		{
			Id: "eu261-passenger-rights",
			FullContent: `## EU261/2004 Passenger Rights (European Flights)

### Regulation Overview

Regulation (EC) No 261/2004 establishes common rules on compensation
and assistance to passengers in the event of denied boarding, flight
cancellation, or long delays on flights departing from EU airports or
operated by EU carriers arriving at EU airports.

### Compensation Amounts

Flight distance determines the compensation amount:
- Short-haul flights (under 1,500 km): 250 EUR
- Medium-haul flights (1,500-3,500 km): 400 EUR
- Long-haul flights (over 3,500 km): 600 EUR

Compensation is reduced by 50% if the airline offers re-routing that
arrives within 2 hours (short-haul), 3 hours (medium-haul), or
4 hours (long-haul) of the original scheduled arrival.

### Delay Thresholds

Passengers are entitled to assistance (meals, refreshments,
communication) when delays exceed:
- 2 hours for short-haul flights
- 3 hours for medium-haul flights
- 4 hours for long-haul flights

A delay of 5+ hours entitles the passenger to a full refund and return
flight.

### Extraordinary Circumstances

Airlines are exempt from compensation (but not assistance) in
extraordinary circumstances including severe weather, air traffic
control restrictions, security risks, and political instability.
Technical problems are generally NOT considered extraordinary
circumstances per ECJ ruling C-549/07.

### Claiming Compensation

Claims must be filed with the operating carrier. The airline must
respond within 6 weeks. If denied, passengers may escalate to the
National Enforcement Body (NEB) of the departure country or file in
small claims court.`,
			Keywords: []string{
				"EU261", "EC261", "261/2004", "european", "regulation",
				"compensation", "passenger rights",
				"extraordinary circumstances",
			},
			SyntheticQueries: []string{
				"what are my rights under EU regulation for delays",
				"european flight compensation rules",
				"how much compensation for cancelled EU flight",
				"EC261 claim process",
			},
		},
		{
			Id: "apis-travel-documentation",
			FullContent: `## APIS and Travel Documentation Requirements

### Advance Passenger Information System (APIS)

All passengers on international flights must provide Advance Passenger
Information (API) data before departure. This data is transmitted to
destination country border agencies. Failure to provide APIS data may
result in denied boarding.

### Required APIS Information

The following data must be provided at check-in or during booking:
- Full legal name (as shown on travel document)
- Date of birth
- Gender
- Travel document number (passport or visa)
- Travel document expiry date
- Nationality/citizenship
- Country of residence

### ESTA Requirements (USA-bound flights)

Passengers from Visa Waiver Program (VWP) countries traveling to the
United States must obtain an approved Electronic System for Travel
Authorization (ESTA) before departure. ESTA applications cost $21 and
are valid for 2 years or until passport expiry. Apply at the official
CBP website at least 72 hours before travel.

### eTA Requirements (Canada-bound flights)

Visa-exempt foreign nationals flying to Canada require an Electronic
Travel Authorization (eTA). The eTA costs CAD $7 and is valid for
5 years or until passport expiry.

### Transit Documentation

Passengers transiting through a country may need a transit visa even if
not leaving the airport. Requirements vary by nationality and transit
country. Check with the airline or destination country embassy before
travel.

### Document Verification

The airline verifies travel documents at check-in. Passengers without
valid documentation for their destination will be denied boarding. The
airline may be fined by destination authorities for transporting
undocumented passengers.`,
			Keywords: []string{
				"APIS", "passport", "documentation", "visa", "ESTA",
				"eTA", "travel document", "advance passenger",
				"border", "CBP",
			},
			SyntheticQueries: []string{
				"what documents do I need for international flights",
				"APIS requirements for international travel",
				"do I need ESTA for USA travel",
				"passport information needed for booking",
			},
		},
		{
			Id: "wifi-inflight-services",
			FullContent: `## In-Flight WiFi and Entertainment Services

### WiFi Availability

In-flight WiFi is available on all domestic flights and select
international routes. WiFi service begins after the aircraft reaches
10,000 feet and is available until descent. Service may be interrupted
during turbulence or over certain oceanic routes.

### WiFi Plans and Pricing

- Browse plan: $8 per flight (email, messaging, basic web browsing)
- Stream plan: $15 per flight (video streaming, video calls, large
  downloads)
- Monthly pass: $49/month (unlimited Browse on all flights)
- Annual pass: $599/year (unlimited Stream on all flights)

Frequent flyer Gold and Platinum members receive complimentary Browse
plan on all flights.

### Supported Devices

WiFi supports up to 3 devices per passenger per plan. Compatible with
laptops, tablets, and smartphones. VPN connections are supported on the
Stream plan only.

### In-Flight Entertainment (IFE)

Seatback screens are available on wide-body aircraft. On narrow-body
aircraft, stream entertainment to your personal device via the airline
portal at entertainment.airline.com. Content includes 200+ movies,
400+ TV episodes, music, podcasts, and games.

### Bluetooth

Bluetooth audio is supported on aircraft equipped with Bluetooth-enabled
IFE systems. Connect your wireless headphones through the seatback
screen pairing menu.

### Restrictions

Voice and video calls are not permitted during flight. Messaging and
text-based communication are allowed on all WiFi plans.`,
			Keywords: []string{
				"WiFi", "wifi", "internet", "streaming",
				"entertainment", "portal", "bluetooth", "IFE",
				"inflight", "onboard",
			},
			SyntheticQueries: []string{
				"does the flight have WiFi",
				"how much does inflight internet cost",
				"can I stream movies on the plane",
				"inflight entertainment options",
			},
		},

		// -----------------------------------------------------------------
		// Saturation policies (14-18): simulate real-world corpus depth
		// -----------------------------------------------------------------
		{
			Id: "seat-selection-upgrades",
			FullContent: `## Seat Selection and Upgrades

### Standard Seat Selection

Seat selection is available at booking or any time before check-in.
Fees vary by seat location and fare class:
- Standard seats (rows 10+): free for all fare classes
- Preferred seats (extra legroom, rows 5-9): $25-$65 per segment
- Exit row seats: $45-$85 per segment (passengers must be 15+ and
  physically able to assist in an emergency)

Gold and Platinum frequent flyer members receive complimentary preferred
seat selection on all bookings. Silver members receive complimentary
preferred seats on domestic flights only.

### Cabin Upgrades

Passengers may upgrade to the next cabin class at booking, at check-in,
or at the gate subject to availability:
- Economy to Premium Economy: from $99 per segment
- Premium Economy to Business: from $299 per segment
- Business to First: from $499 per segment

Upgrade pricing is dynamic and depends on route, demand, and how far in
advance the upgrade is purchased. Upgrades purchased at the gate are
typically the most expensive.

### Standby Upgrades

Frequent flyer Gold and Platinum members are automatically placed on the
complimentary standby upgrade list. Upgrades are cleared 24 hours before
departure based on tier status and booking date. Platinum members receive
priority over Gold members. Standby upgrades are not confirmed until
the gate agent processes the upgrade list.

### Miles-Based Upgrades

Members may use frequent flyer miles to upgrade. Rates vary by route:
- Domestic: 15,000 miles per segment
- International short-haul: 25,000 miles per segment
- International long-haul: 40,000 miles per segment

A co-pay of $75-$200 may apply depending on the original fare class.`,
			Keywords: []string{
				"seat", "upgrade", "preferred", "exit row",
				"legroom", "cabin", "standby upgrade", "miles",
				"seat selection", "premium economy",
			},
			SyntheticQueries: []string{
				"how to select or change my seat",
				"upgrade to business class cost",
				"can I get a free upgrade as gold member",
				"exit row seat requirements and fees",
			},
		},
		{
			Id: "check-in-procedures",
			FullContent: `## Check-In Procedures

### Online Check-In

Online check-in opens 24 hours before scheduled departure and closes
1 hour before departure for domestic flights and 2 hours before for
international flights. Passengers can check in via the airline website
or mobile app. A boarding pass is issued electronically (mobile wallet
or email) or can be printed at home.

### Mobile Check-In

The airline mobile app supports check-in with digital boarding pass
stored in Apple Wallet or Google Pay. Mobile boarding passes include a
scannable QR code accepted at security and boarding gates at all
domestic and most international airports.

### Airport Kiosk Check-In

Self-service kiosks are available at all hub airports. Kiosks accept
confirmation number, frequent flyer number, or credit card lookup.
Bag tags can be printed at the kiosk. Kiosk check-in closes 45 minutes
before departure.

### Counter Check-In

Full-service check-in counters are staffed from 3 hours before
departure. Counter check-in is required for:
- Unaccompanied minors
- Passengers with pets
- Oversized or special baggage
- Passengers needing travel document verification

### Check-In Cutoff Times

Passengers must be checked in and at the gate by the following cutoffs:
- Domestic flights: 30 minutes before departure
- International flights: 60 minutes before departure
- Failure to meet cutoff times may result in seat release and denied
  boarding without compensation`,
			Keywords: []string{
				"check-in", "checkin", "boarding pass", "kiosk",
				"online", "mobile", "counter", "cutoff",
				"gate", "QR code",
			},
			SyntheticQueries: []string{
				"when can I check in for my flight",
				"how to get my boarding pass",
				"check-in cutoff time for international flight",
				"can I check in at the airport kiosk",
			},
		},
		{
			Id: "special-assistance",
			FullContent: `## Special Assistance and Accessibility

### Wheelchair and Mobility Assistance

Wheelchair assistance is available at no charge at all airports.
Passengers should request wheelchair service at least 48 hours before
departure through the airline website, app, or customer service.
Assistance includes curbside to gate, between gates during connections,
and gate to curbside at arrival. Personal wheelchairs are transported
free of charge in the cargo hold and do not count toward baggage
allowance.

### Medical Conditions

Passengers with medical conditions that may affect their ability to
fly should obtain a medical clearance form (MEDIF) from their physician.
The form must be submitted to the airline at least 72 hours before
departure. Supplemental oxygen is available for a fee of $150 per
segment (must be arranged at least 72 hours in advance). Passengers
may not bring personal oxygen concentrators aboard without prior
approval.

### Disability Accommodations

Under the Air Carrier Access Act (ACAA), the airline provides:
- Priority pre-boarding for passengers with disabilities
- Assistance stowing carry-on baggage
- Movable armrests on aisle seats (request at booking)
- Onboard wheelchair for cabin movement on wide-body aircraft
- Seating accommodations for service animals

### Hearing and Vision Impairments

Passengers with hearing impairments may request visual notifications
for boarding announcements. Passengers with vision impairments receive
individual safety briefings from cabin crew. Braille safety cards are
available on request.

### Requesting Assistance

All special assistance requests should be made at least 48 hours before
departure. While the airline accommodates last-minute requests when
possible, advance notice ensures the best experience.`,
			Keywords: []string{
				"wheelchair", "disability", "accessibility",
				"medical", "mobility", "assistance", "ACAA",
				"oxygen", "hearing", "vision", "special needs",
			},
			SyntheticQueries: []string{
				"need wheelchair assistance at the airport",
				"flying with a medical condition",
				"disability accommodations on flights",
				"how to request special assistance for travel",
			},
		},
		{
			Id: "meal-dietary-requests",
			FullContent: `## Meal and Dietary Requests

### Complimentary Meals

Complimentary meal service is provided on:
- All international flights over 4 hours
- Domestic First and Business class flights over 2 hours
- Economy class on domestic flights receives complimentary snacks and
  non-alcoholic beverages only

### Buy-on-Board Menu

Economy class passengers on domestic flights may purchase meals from
the buy-on-board menu. Options include fresh sandwiches ($9-$13),
snack boxes ($7-$10), and premium snacks ($4-$6). Payment is by
credit or debit card only; cash is not accepted.

### Special Dietary Meals

Special meals must be requested at least 24 hours before departure.
Available options include:
- Vegetarian (lacto-ovo)
- Vegan (strict vegetarian)
- Kosher
- Halal
- Gluten-free
- Diabetic
- Low-sodium
- Hindu (non-vegetarian, no beef)
- Child meal (ages 2-12)

### Allergy Information

The airline cannot guarantee an allergen-free environment. Passengers
with severe nut allergies should notify the airline at booking.
A buffer zone of three rows around the passenger will be designated
nut-free, and a pre-boarding announcement will be made requesting
nearby passengers refrain from consuming nut products.

### Pre-Order Premium Meals

On select international routes, Business and First class passengers may
pre-order from the premium dining menu up to 72 hours before departure.
Options include chef-curated entrees, wine pairings, and artisan
desserts.`,
			Keywords: []string{
				"meal", "food", "dietary", "vegetarian", "vegan",
				"kosher", "halal", "gluten-free", "allergy",
				"snack", "beverage", "catering",
			},
			SyntheticQueries: []string{
				"what meals are available on my flight",
				"how to request a special dietary meal",
				"food allergy notification for flight",
				"can I pre-order meals before the flight",
			},
		},
		{
			Id: "lost-luggage-claims",
			FullContent: `## Lost and Delayed Luggage Claims

### Reporting Lost or Delayed Luggage

If your checked luggage does not arrive at your destination, report it
immediately at the Baggage Service Office in the arrivals hall before
leaving the airport. A Property Irregularity Report (PIR) will be
created and you will receive a reference number for tracking. Reports
can also be filed online within 24 hours of arrival.

### Tracking Your Luggage

Track the status of delayed luggage online using your PIR reference
number at baggage.airline.com or through the airline mobile app. Most
delayed bags are located and delivered within 24-48 hours. The airline
provides free delivery to your hotel or home address once the bag is
found.

### Interim Expenses for Delayed Luggage

If your luggage is delayed and you need essential items, the airline
reimburses reasonable interim expenses:
- Domestic travel: up to $50 per day, maximum 3 days ($150 total)
- International travel: up to $100 per day, maximum 5 days ($500 total)

Keep all receipts for reimbursement. Claims must be submitted within
21 days of receiving the delayed luggage.

### Compensation for Lost Luggage

Luggage is considered lost if not found within 21 days of filing the
PIR. Compensation is limited to:
- Domestic flights: up to $3,800 per passenger (DOT maximum)
- International flights: up to 1,288 SDR per passenger (approximately
  $1,700 USD) under the Montreal Convention

Compensation is based on the depreciated value of the contents, not
the original purchase price. Retain proof of purchase for valuable
items.

### Filing a Claim

Submit a written claim with the following:
- PIR reference number
- Itemized list of contents with estimated values
- Proof of purchase for high-value items (receipts, credit card
  statements)
- Receipts for interim expense reimbursement

Claims must be filed within 21 days of the luggage being declared
lost. The airline responds within 30 business days.`,
			Keywords: []string{
				"lost luggage", "delayed baggage", "missing bag",
				"claim", "PIR", "compensation", "tracking",
				"reimbursement", "Montreal Convention",
			},
			SyntheticQueries: []string{
				"my luggage did not arrive what do I do",
				"how to file a lost baggage claim",
				"compensation for lost or delayed luggage",
				"tracking delayed checked bag",
			},
		},
		{
			Id: "group-bookings",
			FullContent: `## Group Booking Policy

### Eligibility

Group rates are available for parties of 10 or more passengers
traveling on the same flight and route. All passengers must be booked
under a single group reservation.

### Pricing and Deposits

- Group fares are quoted upon request and are typically 10-15% below
  published economy fares
- A non-refundable deposit of $100 per passenger is required to hold
  the group reservation
- Full payment is due 60 days before departure
- Failure to pay by the deadline results in automatic cancellation and
  forfeiture of the deposit

### Name Changes

- Passenger names may be added or changed up to 14 days before
  departure at no charge
- Name changes within 14 days of departure incur a $25 fee per change
- The total number of passengers cannot exceed the original group size

### Cancellation

- Cancellations more than 60 days before departure: full deposit
  refund minus $25 processing fee per passenger
- Cancellations 30-60 days before departure: 50% deposit refund
- Cancellations less than 30 days before departure: no refund
- Individual passengers may be removed from the group without
  cancelling the entire booking (minimum 10 passengers must remain)

### Seat Assignments

Group bookings receive seat assignments at check-in. Advance seat
selection is available for an additional $15 per passenger per segment.
The airline makes best efforts to seat group members together but
cannot guarantee adjacent seating on full flights.`,
			Keywords: []string{
				"group", "group booking", "party", "10 or more",
				"deposit", "name change", "group rate",
			},
			SyntheticQueries: []string{
				"booking for a group of passengers",
				"group discount for 15 people traveling together",
				"how to make a group reservation",
				"name changes for group bookings",
			},
		},
	}
}
