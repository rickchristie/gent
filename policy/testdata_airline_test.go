//go:build cgo

package policy

// airlinePolicies returns 10 realistic airline policies for integration testing.
// These simulate a real airline's customer service policy corpus. The purpose is testing
// that PolicySearchTool can cut through noise and surface the most relevant policies.
func airlinePolicies() []*Policy {
	return []*Policy{
		{
			Id: "flight-change-rebooking",
			Description: "Change fees, fare difference, same-day changes," +
				" and FF waivers",
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

If the new flight has a higher fare, the customer pays the difference in addition
to any change fee. If the new flight has a lower fare, the difference is issued as
a travel credit valid for 12 months.

### Frequent Flyer Exceptions

Gold members receive one complimentary change per booking regardless of class.
Platinum members have all change fees waived on every booking.

### Same-Day Changes

Same-day confirmed changes are available for $75 (waived for Gold and above).
Same-day standby is free for all fare classes.`,
			Keywords: []string{
				"change", "reschedule", "modify", "rebook", "fee",
				"same-day", "fare difference",
			},
			SyntheticQueries: []string{
				"customer wants to change their flight",
				"how much does it cost to reschedule",
				"rebooking fee for economy class",
				"can gold members change for free",
			},
		},
		{
			Id: "cancellation-refund",
			Description: "Cancellation fees, refund types, and travel" +
				" credit rules",
			FullContent: `## Cancellation and Refund Policy

### Refundable Tickets (Flex Fare)

Refundable tickets may be cancelled at any time before departure for a full refund
minus a $25 processing fee. Refunds are credited to the original payment method
within 7-10 business days for credit cards and 14-21 business days for bank transfers.

### Non-Refundable Tickets (Standard Fare)

Non-refundable tickets cancelled before departure receive a travel credit equal to
the ticket value minus a $100 cancellation fee. Travel credits expire 12 months from
the original booking date and can be applied to any future booking.

### Basic Economy Tickets

Basic Economy tickets are non-refundable and non-changeable. The full ticket value
is forfeited upon cancellation. No travel credit is issued.

### Airline-Initiated Cancellations

If the airline cancels the flight, the customer receives a full cash refund
regardless of ticket type. No processing fees apply. The refund is processed
automatically within 7 business days.`,
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
			Description: "Carry-on and checked bag limits, fees," +
				" and overweight charges",
			FullContent: `## Baggage Allowance and Fees

### Carry-On Baggage

All passengers may bring one carry-on bag (max 22x14x9 inches) and one personal
item (purse, laptop bag, or small backpack) at no charge.

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

Sports equipment, musical instruments, and pet carriers have separate policies.
Contact customer service for specific requirements and fees.`,
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
			Id: "delay-compensation",
			Description: "Delay assistance, rebooking, and DOT" +
				" refund rules",
			FullContent: `## Flight Delay and Cancellation Compensation

### DOT Refund Rules (Effective 2024)

If the airline cancels a flight or makes a significant schedule change (3+ hours
domestic, 6+ hours international), the passenger is entitled to a full cash refund
to the original payment method. This applies regardless of ticket type. Refunds
must be processed within 7 business days for credit cards.

### Meal and Accommodation Vouchers

For delays of 2-4 hours: complimentary snack and beverage voucher ($15 value).
For delays of 4-8 hours: meal voucher ($25 value).
For overnight delays: hotel accommodation at partner hotels plus meal vouchers.
Transportation between airport and hotel is provided.

### Rebooking on Alternative Flights

Passengers affected by delays or cancellations are rebooked on the next available
flight at no additional cost. If the next available flight is on a partner airline,
the rebooking is handled automatically. Passengers may also request a refund instead
of rebooking.

### No Cash Compensation in the US

Unlike EU261 regulations, US law does not require airlines to pay cash compensation
for delays. The airline provides meals, accommodation, and rebooking as described
above.`,
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
			Id: "frequent-flyer-benefits",
			Description: "Bronze, Silver, Gold, and Platinum tier" +
				" benefits",
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
- Complimentary seat selection including premium economy
- Priority rebooking during irregular operations

### Platinum Tier (100,000+ miles per year)

- All Gold benefits
- 100% bonus miles on all flights
- Three free checked bags on all bookings
- All change and cancellation fees waived
- Complimentary upgrades to next cabin when available (cleared 24 hours before departure)
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
		{
			Id: "24-hour-cancellation",
			Description: "DOT-mandated 24-hour risk-free cancellation" +
				" window",
			FullContent: `## 24-Hour Risk-Free Cancellation

### DOT-Mandated 24-Hour Rule

All tickets booked directly with the airline may be cancelled within 24 hours of
purchase for a full refund, regardless of ticket type (including Basic Economy).
This applies to all bookings made at least 7 days before the scheduled departure.

### Conditions

- The booking must have been made at least 7 days before departure
- The cancellation must be requested within 24 hours of the original booking time
- Applies to all fare classes including Basic Economy
- The refund is processed to the original payment method
- Processing time: 7-10 business days for credit cards

### How to Cancel

Customers can cancel within 24 hours through:
- The airline website (My Trips > Cancel Booking)
- The airline mobile app
- Calling customer service

### Important Note

This 24-hour window does NOT apply to bookings made through third-party travel
agencies or online travel agents. Customers must contact the third party directly
for their cancellation policy.`,
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
			Id: "same-day-change",
			Description: "Same-day confirmed change and standby" +
				" options",
			FullContent: `## Same-Day Flight Changes

### Same-Day Confirmed Change

Passengers may change to an earlier or later flight on the same day and same route
for a $75 fee. The fare difference applies if the new flight has a higher fare.
If the new flight is cheaper, no refund of the difference is provided.

Fee waivers:
- Gold and Platinum frequent flyer members: fee waived
- Business and First class passengers: fee waived

### Same-Day Standby

Same-day standby is available at no charge for all passengers. The passenger keeps
their original confirmed seat and is placed on the standby list for the requested
flight. If a seat becomes available, they are accommodated in the same cabin class.

### Eligibility

- Both options are available only for flights departing on the same calendar day
- The origin and destination must be the same as the original booking
- Not available for Basic Economy tickets
- Cannot be combined with other promotions or discounts`,
			Keywords: []string{
				"same day", "same-day", "standby", "earlier flight",
				"later flight", "confirmed change",
			},
			SyntheticQueries: []string{
				"change to an earlier flight today",
				"can I fly standby on a later flight",
				"same day change fee",
				"switch to different flight today",
			},
		},
		{
			Id: "involuntary-rebooking",
			Description: "Denied boarding compensation and rebooking" +
				" rights",
			FullContent: `## Involuntary Rebooking and Denied Boarding

### Oversold Flight Compensation

If a flight is oversold, the airline first seeks volunteers willing to give up their
seats in exchange for compensation (travel vouchers, typically $200-$800 depending
on route and demand).

### Involuntary Denied Boarding (IDB)

If insufficient volunteers come forward, passengers may be involuntarily denied
boarding. Under DOT rules, compensation is required:
- Arrival delay 0-1 hour: no compensation required
- Arrival delay 1-2 hours (domestic) or 1-4 hours (international): 200% of one-way fare, max $775
- Arrival delay over 2 hours (domestic) or over 4 hours (international): 400% of one-way fare, max $1,550

### Rebooking Priority

Denied passengers are rebooked on the next available flight at no cost. If the
airline cannot provide a seat within 2 hours, the passenger may request a full
refund instead. The airline covers meal and accommodation costs during the wait.

### Who Is Protected

Passengers with confirmed reservations who checked in on time cannot be denied
boarding without compensation. Passengers who arrive late or do not have confirmed
seats are not covered.`,
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
			Description: "Age requirements, service fees, and" +
				" documentation for minors",
			FullContent: `## Unaccompanied Minor Policy

### Age Requirements

- Children under 5: must travel with an adult (18+)
- Children 5-7: unaccompanied minor service required on direct/nonstop flights only
- Children 8-14: unaccompanied minor service required on all flights
- Children 15-17: unaccompanied minor service optional

### Service Fee

The unaccompanied minor service fee is $150 per child per direction (one-way). This
covers escort through the airport, supervision during connections, and handoff to
the designated guardian at arrival.

### Required Documentation

- Completed Unaccompanied Minor form with contact details for both the sending and
  receiving guardians
- Valid photo ID for both guardians
- Gate pass for the sending guardian (provided at check-in)
- The receiving guardian must present photo ID matching the form at arrival

### Restrictions

- Not available on red-eye flights (departing after 9 PM)
- Not available on the last connecting flight of the day
- Maximum of one connection per itinerary
- The child must be checked in at the airport counter (online check-in not available)`,
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
			Description: "In-cabin and cargo pet policies, fees," +
				" and breed restrictions",
			FullContent: `## Pet Travel Policy

### In-Cabin Pets

Small dogs, cats, and household birds may travel in the cabin in an airline-approved
carrier that fits under the seat. Fee: $125 per pet per direction. Maximum one pet
carrier per passenger. The combined weight of pet and carrier must not exceed 20 lbs.

### Cargo Pets

Larger pets travel in the pressurized, temperature-controlled cargo hold. Fee: $200
per pet per direction. Pets must be in an IATA-compliant hard-sided kennel. Available
on flights under 12 hours with temperatures between 45-85°F at origin, destination,
and all connection points.

### Breed Restrictions

Brachycephalic (snub-nosed) breeds are not accepted in cargo due to respiratory
risks. This includes Bulldogs, Pugs, Boston Terriers, Pekingese, Shih Tzu, and
similar breeds. These breeds may travel in-cabin only if they meet the carrier
size requirements.

### Service and Emotional Support Animals

Trained service dogs travel in the cabin at no charge. Passengers must submit the
DOT Service Animal Transportation Form at least 48 hours before departure.
Emotional support animals are not given special accommodations and must follow
standard pet travel policies.

### Health Documentation

A veterinary health certificate issued within 10 days of travel is required for all
pets. International travel may require additional documentation including rabies
vaccination records and import permits.`,
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

		// --- BM25 scenario policies ---
		// These test scenarios where BM25 should contribute significantly:
		// identifiers, rare acronyms, numeric discrimination, short queries.

		{
			Id: "eu261-passenger-rights",
			Description: "EU261/2004 compensation amounts and delay" +
				" thresholds",
			FullContent: `## EU261/2004 Passenger Rights (European Flights)

### Regulation Overview

Regulation (EC) No 261/2004 establishes common rules on compensation and assistance
to passengers in the event of denied boarding, flight cancellation, or long delays
on flights departing from EU airports or operated by EU carriers arriving at EU
airports.

### Compensation Amounts

Flight distance determines the compensation amount:
- Short-haul flights (under 1,500 km): €250
- Medium-haul flights (1,500-3,500 km): €400
- Long-haul flights (over 3,500 km): €600

Compensation is reduced by 50% if the airline offers re-routing that arrives within
2 hours (short-haul), 3 hours (medium-haul), or 4 hours (long-haul) of the original
scheduled arrival.

### Delay Thresholds

Passengers are entitled to assistance (meals, refreshments, communication) when
delays exceed:
- 2 hours for short-haul flights
- 3 hours for medium-haul flights
- 4 hours for long-haul flights

A delay of 5+ hours entitles the passenger to a full refund and return flight.

### Extraordinary Circumstances

Airlines are exempt from compensation (but not assistance) in extraordinary
circumstances including severe weather, air traffic control restrictions, security
risks, and political instability. Technical problems are generally NOT considered
extraordinary circumstances per ECJ ruling C-549/07.

### Claiming Compensation

Claims must be filed with the operating carrier. The airline must respond within
6 weeks. If denied, passengers may escalate to the National Enforcement Body (NEB)
of the departure country or file in small claims court.`,
			Keywords: []string{
				"EU261", "EC261", "261/2004", "european", "regulation",
				"compensation", "passenger rights", "extraordinary circumstances",
			},
			SyntheticQueries: []string{
				"what are my rights under EU regulation for flight delays",
				"european flight compensation rules",
				"how much compensation for cancelled EU flight",
				"EC261 claim process",
			},
		},
		{
			Id: "apis-travel-documentation",
			Description: "APIS, ESTA, eTA, and passport" +
				" requirements",
			FullContent: `## APIS and Travel Documentation Requirements

### Advance Passenger Information System (APIS)

All passengers on international flights must provide Advance Passenger Information
(API) data before departure. This data is transmitted to destination country border
agencies. Failure to provide APIS data may result in denied boarding.

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

Passengers from Visa Waiver Program (VWP) countries traveling to the United States
must obtain an approved Electronic System for Travel Authorization (ESTA) before
departure. ESTA applications cost $21 and are valid for 2 years or until passport
expiry. Apply at the official CBP website at least 72 hours before travel.

### eTA Requirements (Canada-bound flights)

Visa-exempt foreign nationals flying to Canada require an Electronic Travel
Authorization (eTA). The eTA costs CAD $7 and is valid for 5 years or until
passport expiry.

### Transit Documentation

Passengers transiting through a country may need a transit visa even if not leaving
the airport. Requirements vary by nationality and transit country. Check with the
airline or destination country embassy before travel.

### Document Verification

The airline verifies travel documents at check-in. Passengers without valid
documentation for their destination will be denied boarding. The airline may be
fined by destination authorities for transporting undocumented passengers.`,
			Keywords: []string{
				"APIS", "passport", "documentation", "visa", "ESTA", "eTA",
				"travel document", "advance passenger", "border", "CBP",
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
			Description: "WiFi plans, pricing, entertainment," +
				" and Bluetooth",
			FullContent: `## In-Flight WiFi and Entertainment Services

### WiFi Availability

In-flight WiFi is available on all domestic flights and select international routes.
WiFi service begins after the aircraft reaches 10,000 feet and is available until
descent. Service may be interrupted during turbulence or over certain oceanic routes.

### WiFi Plans and Pricing

- Browse plan: $8 per flight (email, messaging, basic web browsing)
- Stream plan: $15 per flight (video streaming, video calls, large downloads)
- Monthly pass: $49/month (unlimited Browse on all flights)
- Annual pass: $599/year (unlimited Stream on all flights)

Frequent flyer Gold and Platinum members receive complimentary Browse plan on all
flights.

### Supported Devices

WiFi supports up to 3 devices per passenger per plan. Compatible with laptops,
tablets, and smartphones. VPN connections are supported on the Stream plan only.

### In-Flight Entertainment (IFE)

Seatback screens are available on wide-body aircraft. On narrow-body aircraft, stream
entertainment to your personal device via the airline portal at
entertainment.airline.com. Content includes 200+ movies, 400+ TV episodes, music,
podcasts, and games.

### Bluetooth

Bluetooth audio is supported on aircraft equipped with Bluetooth-enabled IFE systems.
Connect your wireless headphones through the seatback screen pairing menu.

### Restrictions

Voice and video calls are not permitted during flight. Messaging and text-based
communication are allowed on all WiFi plans.`,
			Keywords: []string{
				"WiFi", "wifi", "internet", "streaming", "entertainment",
				"portal", "bluetooth", "IFE", "inflight", "onboard",
			},
			SyntheticQueries: []string{
				"does the flight have WiFi",
				"how much does inflight internet cost",
				"can I stream movies on the plane",
				"inflight entertainment options",
			},
		},
	}
}
