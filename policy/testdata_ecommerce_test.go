//go:build cgo

package policy

func ecommercePolicies() []*Policy {
	return []*Policy{
		{
			Id: "standard-return",
			Description: "30-day return window, conditions," +
				" and exceptions",
			FullContent: `## Standard Return Policy

All items purchased from TechEdge Electronics may be returned within
**30 days** of the original purchase date for a full refund, subject to
the conditions below.

### Conditions

- Items must be in their original packaging with all accessories,
  manuals, and free gifts included.
- All manufacturer tags and labels must be attached and unaltered.
- A valid receipt or order confirmation email is required.
- Items must show no signs of wear, damage, or modification beyond
  initial inspection.

### Exceptions — Non-Returnable Items

- **Opened software, video games, and digital downloads** cannot be
  returned once the seal is broken.
- **Personalized or custom-built items** (e.g., engraved accessories,
  BTO desktops) are final sale.
- **Clearance items** marked "Final Sale" at time of purchase are
  non-returnable.

### Restocking Fees

Certain categories are subject to a restocking fee deducted from the
refund amount:

- Opened laptops and desktops: **15% restocking fee**.
- Drones and aerial equipment: **15% restocking fee**.
- Opened cameras and lenses: **10% restocking fee**.
- All other categories: **no restocking fee** if returned in original
  condition.

Returns may be initiated online or at any TechEdge store location.`,
			Keywords: []string{
				"return", "refund", "restocking", "fee", "non-returnable",
				"packaging", "receipt", "30 days", "final sale",
			},
			SyntheticQueries: []string{
				"customer wants to return a product",
				"what is the return window for purchases",
				"can I return an opened laptop",
				"which items cannot be returned",
				"is there a restocking fee for returns",
			},
		},
		{
			Id: "refund-processing",
			Description: "Refund timeline by payment method",
			FullContent: `## Refund Processing

Once a returned item has been received and inspected, refunds are
processed according to the original payment method.

### Timeline by Payment Method

- **Credit card**: Refund posted within **5–7 business days** after
  processing. Allow one additional billing cycle for the credit to
  appear on your statement.
- **Debit card**: Refund posted within **7–10 business days**. Funds
  return to the original checking account.
- **PayPal or digital wallet**: Refund posted within **3–5 business
  days** to the originating account.
- **Store credit or gift card**: Refund applied **immediately** as
  store credit to the customer's TechEdge account.
- **Cash (in-store purchases)**: Refund issued in cash at the register
  for amounts under $500. Amounts of $500 or more are refunded via
  company check mailed within 10 business days.

### Partial Refunds

A partial refund may be issued when:

- Return is missing original accessories, cables, or packaging.
  Deduction is based on the replacement cost of the missing items.
- Product shows signs of use beyond initial inspection, assessed at
  up to 20% of the purchase price.

### Payment Method Preference

Refunds are always issued to the **original payment method**. We
cannot redirect a refund to a different card or account. If the
original payment method is no longer active, a store credit will be
issued instead.`,
			Keywords: []string{
				"refund", "processing", "timeline", "credit card", "debit",
				"PayPal", "store credit", "partial refund", "payment method",
			},
			SyntheticQueries: []string{
				"how long does a refund take",
				"when will I get my money back",
				"can I get a refund to a different card",
				"partial refund for missing accessories",
				"refund for cash purchase over $500",
			},
		},
		{
			Id: "extended-warranty",
			Description: "Coverage tiers, claims process," +
				" and exclusions",
			FullContent: `## Extended Warranty — TechEdge Protection Plans

TechEdge offers three tiers of extended warranty coverage that begin
after the manufacturer warranty expires.

### Plan Tiers

- **Essential Plan — $49**: Covers mechanical and electrical failure
  for **2 years**. Includes free diagnostic service and parts/labor
  for covered repairs.
- **Plus Plan — $99**: Everything in Essential, plus **accidental
  damage from handling** (drops, spills, cracked screens) for
  **3 years**. Includes one free battery replacement for laptops.
- **Premium Plan — $199**: Everything in Plus, plus **power surge
  protection** and **food/liquid submersion** for **4 years**.
  Includes up to two incidents of accidental damage per year and
  free loaner device during repairs exceeding 5 business days.

### Claim Process

1. File a claim online or call **1-800-555-TECH** (8 AM–8 PM EST).
2. A technician will triage the issue within **24 hours**.
3. Ship the item using the prepaid label provided, or bring it to
   any TechEdge store.
4. Repairs are completed within **7–14 business days**. If the item
   cannot be repaired, a replacement of equal or greater value is
   provided.

### Exclusions

- **Cosmetic damage** that does not affect functionality (scratches,
  dents, discoloration).
- **Lost or stolen items** — protection plans cover defects and
  damage, not loss.
- **Unauthorized modifications** or repairs performed by third-party
  service providers.`,
			Keywords: []string{
				"warranty", "protection plan", "accidental damage",
				"power surge", "mechanical failure", "claim",
				"repair", "replacement", "coverage", "extended",
			},
			SyntheticQueries: []string{
				"customer wants to purchase a protection plan",
				"what does the extended warranty cover",
				"how to file a warranty claim",
				"is accidental damage covered under warranty",
				"warranty does not cover cosmetic damage",
			},
		},
		{
			Id: "price-match",
			Description: "Eligible competitors, process," +
				" and exclusions",
			FullContent: `## Price Match Guarantee

TechEdge will match the current selling price of an identical item
from select competitors. The price match applies at the time of
purchase or within **15 days** after purchase.

### Eligible Competitors

Price matches are honored against the following retailers:

- Amazon.com (sold and shipped by Amazon only)
- Walmart.com and Walmart stores
- Newegg.com (sold and shipped by Newegg only)
- B&H Photo (bhphotovideo.com and NYC store)
- Target.com and Target stores

### Requirements

- The product must be the **exact same model number**, brand, and
  color — no substitutions.
- The item must be **new and in stock** at the competitor at the time
  the price match is requested.
- The competitor price must be a published, verifiable price. We will
  confirm availability and pricing via the competitor's website or
  by calling their store.
- Price matches are limited to **one per customer per item**.

### Exclusions

- **Third-party or marketplace sellers** (e.g., Amazon Marketplace,
  Walmart Marketplace, eBay sellers) are not eligible.
- **Bundle deals, loyalty discounts, coupon stacking**, and
  **clearance/liquidation** prices are excluded.
- **Pricing errors** on competitor websites are not eligible.
- **Membership-only prices** (e.g., Costco, Sam's Club) are excluded.

### Post-Purchase Adjustment

If the competitor's price drops within **15 days** of your TechEdge
purchase, bring your receipt to any store or contact support for a
refund of the difference.`,
			Keywords: []string{
				"price match", "competitor", "price adjustment",
				"Amazon", "Walmart", "Newegg", "B&H",
				"lowest price", "price guarantee",
			},
			SyntheticQueries: []string{
				"customer found a lower price at another store",
				"can we match Amazon's price on this item",
				"price match request after purchase",
				"does price match apply to marketplace sellers",
				"which competitors qualify for price matching",
			},
		},
		{
			Id: "damaged-defective",
			Description: "Photo evidence requirements," +
				" replacement vs refund",
			FullContent: `## Damaged or Defective Items

If you receive an item that is damaged during shipping or has a
manufacturer defect, TechEdge will resolve the issue at no cost
to you.

### Reporting Window

All damage or defect claims must be reported within **48 hours** of
delivery. After 48 hours, the item falls under the standard return
policy or manufacturer warranty.

### Required Documentation

- **Photographs**: Submit clear photos of the damage or defect,
  including the shipping box, packing materials, and the item
  itself. At least 3 photos are required.
- **Order number** and **date of delivery**.
- A brief written description of the issue.

Submit documentation via the "Report a Problem" form on your order
detail page, or email support@techedge.com.

### Resolution Options

- **Replacement**: A replacement unit is shipped within **1–2
  business days** via express shipping at no charge. You may keep
  using the defective item until the replacement arrives.
- **Full refund**: If a replacement is not available or preferred,
  a full refund is issued within **3–5 business days**.

### Return Shipping

- **Shipping damage**: TechEdge provides a **prepaid return label**
  at no cost. A carrier pickup can be scheduled for large items.
- **Manufacturer defect**: Same prepaid return process. If the
  defect manifests after 48 hours but within **30 days**, standard
  return policy applies with no restocking fee.

Do not dispose of damaged packaging until the claim is resolved.`,
			Keywords: []string{
				"damaged", "defective", "broken", "shipping damage",
				"manufacturer defect", "DOA", "dead on arrival",
				"replacement", "photo documentation",
			},
			SyntheticQueries: []string{
				"customer received a broken or damaged item",
				"item arrived defective out of the box",
				"how to report shipping damage",
				"does the customer get free return shipping for defects",
				"replacement vs refund for damaged product",
			},
		},
		{
			Id: "exchange",
			Description: "Size/color exchange process and price" +
				" difference handling",
			FullContent: `## Exchange Policy

TechEdge allows exchanges within **30 days** of the original
purchase date, subject to the conditions below.

### Same Product Exchange

You may exchange an item for the **same product in a different
size, color, or configuration** at no additional charge, provided:

- The item is **unused and in original packaging** with all
  accessories and manuals.
- Tags and labels are attached and unaltered.
- A valid receipt or order confirmation is presented.

If the replacement variant has a higher retail price, you pay the
difference. If it is lower, the difference is refunded to the
original payment method.

### Different Product Exchange

Exchanging for a **different product** is handled as a **return
plus new purchase**:

1. The original item is returned under the standard return policy
   (including any applicable restocking fees).
2. The new item is purchased as a separate transaction.
3. Any applicable promotions or bundle pricing from the original
   order do not transfer to the new purchase.

### In-Store vs. Online

- **In-store exchanges**: Completed immediately at any TechEdge
  location, stock permitting.
- **Online exchanges**: Initiate via your account. The replacement
  ships once the returned item is received, or immediately if you
  opt for a **hold charge** on your payment method (released upon
  return receipt).

### Condition Requirements

Items returned for exchange must meet the same condition standards
as the standard return policy. Items showing signs of use may be
subject to a partial value adjustment.`,
			Keywords: []string{
				"exchange", "swap", "different color", "different size",
				"replacement", "same product", "trade", "switch",
			},
			SyntheticQueries: []string{
				"customer wants to exchange for a different color",
				"can I swap this for a different size",
				"exchange for a completely different product",
				"how does online exchange work",
				"exchange an opened item for the same model",
			},
		},
		{
			Id: "store-credit",
			Description: "Credit balance rules, no expiration," +
				" non-transferable",
			FullContent: `## Store Credit Policy

Store credit is issued as a digital balance on the customer's
TechEdge account and may be used for any future purchase.

### When Store Credit Is Issued

- **Returns without a receipt**: If a customer cannot provide a
  receipt or order confirmation, the return is processed as store
  credit at the item's **current selling price** (not the original
  purchase price).
- **Refund to expired payment method**: When the original credit
  or debit card is closed or expired and cannot accept a refund.
- **Voluntary request**: Customers may opt for store credit instead
  of a refund to their original payment method.

### Terms and Conditions

- Store credit **does not expire** and carries no maintenance or
  dormancy fees.
- Store credit is **non-transferable** — it can only be redeemed
  by the account holder.
- Store credit **cannot be redeemed for cash**, check, or gift
  card, except where required by law.
- The current balance is visible at any time under "My Account >
  Store Credit" on the TechEdge website or app.

### Using Store Credit

- Store credit is applied automatically at checkout when a balance
  is available. Customers may choose to save it for a later order.
- If the order total exceeds the store credit balance, the remaining
  amount is charged to the selected payment method.
- If the order total is less than the store credit balance, the
  unused portion remains on the account.

### Disputes

If you believe your store credit balance is incorrect, contact
support with your account email and the relevant order numbers.
Adjustments are typically resolved within **2 business days**.`,
			Keywords: []string{
				"store credit", "credit balance", "no receipt",
				"non-transferable", "account balance",
				"cannot redeem for cash",
			},
			SyntheticQueries: []string{
				"customer returning without a receipt",
				"how does store credit work",
				"can store credit be converted to cash",
				"check store credit balance",
				"store credit issued instead of refund",
			},
		},
		{
			Id: "shipping-delivery",
			Description: "Shipping tiers, tracking, and lost" +
				" package claims",
			FullContent: `## Shipping & Delivery

TechEdge ships to all 50 U.S. states, Washington D.C., and APO/FPO
addresses. Orders are processed and shipped within **1 business day**
of payment confirmation.

### Shipping Options

- **Standard Shipping (5–7 business days)**: Free on orders over
  **$35**. Orders under $35 are charged a flat **$5.99**.
- **Express Shipping (2–3 business days)**: **$9.99** flat rate,
  regardless of order size or weight.
- **Overnight Shipping (next business day)**: **$19.99** flat rate.
  Orders placed before **2:00 PM EST** on business days ship same
  day. Weekend and holiday orders ship the next business day.
- **In-Store Pickup (same day)**: Free. Orders are ready within
  **2 hours** of confirmation at the selected store location.

### Order Tracking

A tracking number is emailed within **4 hours** of shipment. Track
your package at techedge.com/track or via the carrier's website.

### Delivery Issues

- **Lost packages**: If tracking shows "delivered" but you have not
  received the package, contact support within **30 days** of the
  delivery date. TechEdge will file a carrier claim and either
  reship the order or issue a full refund.
- **Stolen packages**: We recommend using signature confirmation
  ($2.99 add-on) for orders over $200. Without signature
  confirmation, TechEdge is not liable for theft after delivery.
- **Incorrect address**: If a package is returned due to an
  incorrect address provided by the customer, a reshipment fee
  of **$7.99** applies.

### Restrictions

Lithium battery products (laptops, power banks) cannot ship via
air to APO/FPO addresses and will default to ground shipping.`,
			Keywords: []string{
				"shipping", "delivery", "tracking", "express",
				"overnight", "lost package", "free shipping",
				"in-store pickup", "shipping cost",
			},
			SyntheticQueries: []string{
				"how long does standard shipping take",
				"is there free shipping on my order",
				"my package was lost or not delivered",
				"how much does overnight shipping cost",
				"order tracking information",
			},
		},
		{
			Id: "loyalty-program",
			Description: "Points earning, redemption, and" +
				" membership tiers",
			FullContent: `## TechEdge Rewards — Loyalty Program

TechEdge Rewards is a free loyalty program that lets customers earn
points on every purchase and redeem them for rewards.

### Earning Points

- Members earn **1 point per dollar** spent on eligible purchases
  (before tax and shipping).
- Bonus point events: Periodically, TechEdge runs **double or
  triple point** promotions on select categories or store-wide.
- Points are credited to the member's account within **24 hours**
  of purchase (or upon delivery for shipped orders).
- Points are **not earned** on gift card purchases, warranty plans,
  or items paid for with store credit.

### Redeeming Points

- Every **250 points** earned can be redeemed for a **$5 reward
  certificate**.
- Reward certificates are applied at checkout and can be combined
  with other payment methods.
- Points expire **12 months** after the date they were earned if
  the account has had no qualifying purchase activity.

### Membership Tiers

- **Silver (default)**: Earned upon enrollment. 1x points earning.
  Access to member-only sale events.
- **Gold (2,000 points/year)**: **1.5x points earning**. Free
  express shipping on all orders. Extended return window of
  **45 days**.
- **Platinum (5,000 points/year)**: **2x points earning**. Free
  overnight shipping. Extended return window of **60 days**.
  Dedicated phone support line with priority queue. Early access
  to product launches.

### Account Management

Manage your tier status, point balance, and reward certificates at
techedge.com/rewards or in the TechEdge mobile app. Tier status
resets annually on the enrollment anniversary date.`,
			Keywords: []string{
				"loyalty", "rewards", "points", "tier", "Silver",
				"Gold", "Platinum", "redeem", "membership",
				"reward certificate",
			},
			SyntheticQueries: []string{
				"how does the loyalty rewards program work",
				"how many points do I need for a reward",
				"what are the benefits of Gold or Platinum tier",
				"do points expire",
				"customer asking about earning points on a purchase",
			},
		},
		{
			Id: "gift-card",
			Description: "Purchase limits, no expiration," +
				" and replacement",
			FullContent: `## Gift Card Policy

TechEdge gift cards are available for purchase online and at all
store locations.

### Purchase Options

- Gift cards are available in any denomination from **$10 to $500**.
- **Physical gift cards** can be purchased in-store or shipped
  (standard shipping rates apply).
- **Digital gift cards (e-gift cards)** are delivered via email
  within **15 minutes** of purchase. A custom delivery date can
  be scheduled up to 90 days in advance.

### Terms and Conditions

- Gift cards **do not expire** and carry **no activation,
  maintenance, or dormancy fees**.
- Gift cards are **non-refundable** and cannot be returned for
  cash or store credit, except where required by applicable law.
- Gift cards **cannot be used to purchase other gift cards**.
- A maximum of **4 gift cards** may be applied to a single
  transaction.

### Balance Information

- Check your balance online at techedge.com/giftcard, in the
  TechEdge mobile app, or at any store register.
- The remaining balance is printed on in-store receipts after
  each use.

### Lost or Stolen Cards

- **Physical cards**: A replacement can be issued with **proof of
  purchase** (receipt or order confirmation showing the gift card
  transaction). The original card is deactivated and the remaining
  balance is transferred to the replacement. A **$5 replacement
  fee** applies.
- **E-gift cards**: Contact support to resend the email to the
  original recipient address at no charge.
- TechEdge is not responsible for lost, stolen, or unauthorized
  use of gift cards. Treat gift cards like cash.

### Corporate & Bulk Orders

Orders of **10 or more gift cards** totaling **$1,000+** qualify
for a **5% bulk discount**. Contact corporate@techedge.com for
details.`,
			Keywords: []string{
				"gift card", "e-gift card", "balance", "no expiration",
				"non-refundable", "lost card", "replacement",
				"bulk order", "denomination",
			},
			SyntheticQueries: []string{
				"customer wants to buy a gift card",
				"can a gift card be refunded",
				"how to check gift card balance",
				"lost or stolen gift card replacement",
				"do gift cards expire or have fees",
			},
		},

		// --- BM25 scenario policies ---
		// These test scenarios where BM25 should contribute significantly:
		// identifiers, rare acronyms, numeric discrimination, semantic confusion.

		{
			Id: "rma-returns-authorization",
			Description: "RMA number process for electronics" +
				" and high-value items",
			FullContent: `## Return Merchandise Authorization (RMA) Process

### When RMA is Required

A Return Merchandise Authorization number is required for all returns of:
- Electronics and computers (any value)
- Items over $500 in value
- Items being returned for warranty repair or replacement
- Items being returned after the standard 30-day return window (if approved)

Standard returns under $500 for non-electronics do not require an RMA number.

### How to Obtain an RMA

1. Log into your account and navigate to Order History
2. Select the item and click "Request Return Authorization"
3. Select the reason for return and provide description
4. Upload photos if the item is damaged or defective
5. The RMA number and prepaid shipping label are generated within 24 hours

Alternatively, contact customer service to request an RMA by phone.

### RMA Validity

- RMA numbers are valid for 14 days from issuance
- Items must be shipped within this 14-day window
- Expired RMA numbers cannot be reactivated — a new RMA must be requested
- Each RMA covers one item; multi-item returns need separate RMA numbers

### Shipping with RMA

- Use ONLY the prepaid shipping label provided with the RMA
- Write the RMA number on the outside of the package
- Ship via the designated carrier (UPS for domestic, DHL for international)
- Keep the tracking number as proof of shipment

### Processing Time

Returns with valid RMA numbers are processed within 3-5 business days of receipt.
Returns without RMA numbers are held for up to 14 business days while we locate
the order.`,
			Keywords: []string{
				"RMA", "return authorization", "return merchandise",
				"authorization number", "prepaid label", "warranty return",
			},
			SyntheticQueries: []string{
				"how do I get an RMA number",
				"return authorization process for electronics",
				"do I need authorization to return an item",
				"RMA number for warranty return",
			},
		},
		{
			Id: "electronics-return-15day",
			Description: "15-day return window and restocking" +
				" fees for electronics",
			FullContent: `## Electronics Return Policy (15-Day Window)

### Shortened Return Window

All electronics and computer equipment must be returned within 15 days of delivery.
This shortened window applies to:
- Laptops, desktops, and tablets
- Smartphones and smartwatches
- Televisions and monitors
- Cameras and drones
- Gaming consoles
- Audio equipment over $100

Items returned after 15 days but within 30 days may be eligible for store credit
only (no cash refund), subject to a 15% restocking fee.

### Condition Requirements

Electronics must be returned in like-new condition:
- All original packaging, manuals, and accessories included
- No visible scratches, dents, or damage
- Factory seal intact on software and games (opened software is non-returnable)
- Activation lock removed (Apple devices must be signed out of iCloud)

### Restocking Fees

- Unopened, sealed electronics: no restocking fee
- Opened electronics in like-new condition: 10% restocking fee
- Opened electronics with missing accessories: 15% restocking fee
- Items not in original packaging: 25% restocking fee

### Non-Returnable Electronics

The following electronics cannot be returned once opened:
- Software and digital downloads
- Prepaid cards and phone top-ups
- Custom-configured or built-to-order computers
- Drones that have been registered with the FAA`,
			Keywords: []string{
				"electronics", "15 day", "15-day", "computer", "laptop",
				"phone", "tablet", "restocking fee", "television", "camera",
			},
			SyntheticQueries: []string{
				"how long do I have to return electronics",
				"15 day return policy for laptops",
				"can I return a TV after opening it",
				"restocking fee for electronics return",
			},
		},
		{
			Id: "tax-reporting-1099k",
			Description: "IRS 1099-K reporting thresholds and" +
				" TIN requirements",
			FullContent: `## Tax Reporting and Form 1099-K

### IRS Reporting Requirements

Under IRS regulations, we are required to report certain transactions via
Form 1099-K. Beginning January 2024, the IRS threshold for 1099-K reporting
is $5,000 in gross payments. This threshold decreases to $2,500 in 2025 and
$600 in 2026 and beyond.

### Who Receives Form 1099-K

You will receive a Form 1099-K if:
- You sold items through our marketplace totaling $5,000+ in a calendar year
- You received payments through our payment processing system above threshold
- You participated in our affiliate program with earnings above the threshold

Form 1099-K is mailed by January 31 and is also available in your account under
Tax Documents.

### What Form 1099-K Reports

The form reports gross payment amounts before any adjustments for:
- Refunds and returns
- Shipping costs
- Fees and commissions
- Discounts and promotions

You may need to adjust the gross amount on your tax return to account for these
deductions.

### Tax Identification Number (TIN)

Sellers must provide a valid TIN (SSN or EIN) to avoid backup withholding. If no
TIN is on file, 24% of payments are withheld and remitted to the IRS. Update your
TIN in Account Settings > Tax Information.

### State-Specific Requirements

Some states have lower reporting thresholds. Massachusetts, Vermont, Virginia, and
Maryland require reporting at $600 regardless of federal thresholds.`,
			Keywords: []string{
				"1099-K", "1099", "tax", "IRS", "reporting", "TIN",
				"SSN", "EIN", "withholding", "marketplace", "seller",
			},
			SyntheticQueries: []string{
				"will I get a 1099 form for my sales",
				"IRS tax reporting requirements for marketplace sellers",
				"1099-K threshold amount",
				"do I need to report taxes on my sales",
			},
		},
	}
}
