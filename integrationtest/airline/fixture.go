package airline

import (
	"context"
	"fmt"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/integrationtest/testutil"
	"github.com/rickchristie/gent/schema"
)

// -----------------------------------------------------------------------------
// Data Types
// -----------------------------------------------------------------------------

type Customer struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Email         string   `json:"email"`
	Phone         string   `json:"phone"`
	FrequentFlyer string   `json:"frequent_flyer_tier"` // bronze, silver, gold, platinum
	BookingIDs    []string `json:"booking_ids"`
}

type Flight struct {
	FlightNumber    string    `json:"flight_number"`
	Origin          string    `json:"origin"`
	Destination     string    `json:"destination"`
	DepartureTime   time.Time `json:"departure_time"`
	ArrivalTime     time.Time `json:"arrival_time"`
	Aircraft        string    `json:"aircraft"`
	Status          string    `json:"status"` // scheduled, boarding, departed, arrived, cancelled, delayed
	AvailableSeats  int       `json:"available_seats"`
	EconomyPrice    float64   `json:"economy_price"`
	BusinessPrice   float64   `json:"business_price"`
	FirstClassPrice float64   `json:"first_class_price"`
}

type Booking struct {
	BookingID      string    `json:"booking_id"`
	CustomerID     string    `json:"customer_id"`
	FlightNumber   string    `json:"flight_number"`
	SeatNumber     string    `json:"seat_number"`
	Class          string    `json:"class"`  // economy, business, first
	Status         string    `json:"status"` // confirmed, cancelled, checked_in
	BookingDate    time.Time `json:"booking_date"`
	TotalPrice     float64   `json:"total_price"`
	MealPreference string    `json:"meal_preference"`
}

type Seat struct {
	SeatNumber  string `json:"seat_number"`
	Class       string `json:"class"`
	IsAvailable bool   `json:"is_available"`
	IsWindow    bool   `json:"is_window"`
	IsAisle     bool   `json:"is_aisle"`
	HasExtraLeg bool   `json:"has_extra_legroom"`
}

type Policy struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// -----------------------------------------------------------------------------
// Tool Input Types
// -----------------------------------------------------------------------------

type GetCustomerInfoInput struct {
	CustomerID string `json:"customer_id"`
	Email      string `json:"email"`
}

type GetBookingInfoInput struct {
	BookingID string `json:"booking_id"`
}

type GetFlightInfoInput struct {
	FlightNumber string `json:"flight_number"`
}

type GetFlightSeatsInfoInput struct {
	FlightNumber  string `json:"flight_number"`
	Class         string `json:"class"`
	AvailableOnly bool   `json:"available_only"`
}

type SearchAirlinePolicyInput struct {
	Keyword string `json:"keyword"`
}

type SearchFlightScheduleInput struct {
	Origin      string `json:"origin"`
	Destination string `json:"destination"`
	Date        string `json:"date"`
}

type RescheduleBookingInput struct {
	BookingID       string `json:"booking_id"`
	NewFlightNumber string `json:"new_flight_number"`
	NewSeatNumber   string `json:"new_seat_number"`
}

type CancelBookingInput struct {
	BookingID string `json:"booking_id"`
	Reason    string `json:"reason"`
}

type SendNotificationInput struct {
	CustomerID string `json:"customer_id"`
	Method     string `json:"method"`
	Subject    string `json:"subject"`
	Message    string `json:"message"`
}

// -----------------------------------------------------------------------------
// Tool Result Types
// -----------------------------------------------------------------------------

// RescheduleBookingResult is the result of rescheduling a booking.
type RescheduleBookingResult struct {
	Success        bool    `json:"success"`
	OldFlightNum   string  `json:"old_flight_number"`
	NewFlightNum   string  `json:"new_flight_number"`
	NewSeatNumber  string  `json:"new_seat_number"`
	ChangeFee      float64 `json:"change_fee"`
	FareDifference float64 `json:"fare_difference"`
	FareCredit     float64 `json:"fare_credit"`
	TotalCharge    float64 `json:"total_charge"`
	Message        string  `json:"message"`
}

// CancelBookingResult is the result of cancelling a booking.
type CancelBookingResult struct {
	Success      bool    `json:"success"`
	BookingID    string  `json:"booking_id"`
	RefundAmount float64 `json:"refund_amount"`
	RefundType   string  `json:"refund_type"` // cash, credit
	Message      string  `json:"message"`
}

// SendNotificationResult is the result of sending a notification.
type SendNotificationResult struct {
	Success bool   `json:"success"`
	Method  string `json:"method"`
	Message string `json:"message"`
}

// -----------------------------------------------------------------------------
// AirlineFixture
// -----------------------------------------------------------------------------

// AirlineFixture provides a complete airline mock environment with dynamic dates.
// All dates in the mock data are calculated relative to "today" from the TimeProvider,
// ensuring consistent behavior in LLM integration tests regardless of when they run.
type AirlineFixture struct {
	timeProvider gent.TimeProvider

	// Instance data - not shared across fixtures
	customers map[string]*Customer
	flights   map[string]*Flight
	bookings  map[string]*Booking
	seats     map[string][]Seat
	policies  []Policy
}

// NewAirlineFixture creates a new AirlineFixture with dynamic dates.
// If tp is nil, uses gent.DefaultTimeProvider.
func NewAirlineFixture(tp gent.TimeProvider) *AirlineFixture {
	if tp == nil {
		tp = gent.NewDefaultTimeProvider()
	}

	f := &AirlineFixture{
		timeProvider: tp,
		customers:    make(map[string]*Customer),
		flights:      make(map[string]*Flight),
		bookings:     make(map[string]*Booking),
		seats:        make(map[string][]Seat),
	}

	f.initializeData()
	return f
}

// TimeProvider returns the fixture's time provider.
func (f *AirlineFixture) TimeProvider() gent.TimeProvider {
	return f.timeProvider
}

// Today returns the fixture's current date for reference.
func (f *AirlineFixture) Today() time.Time {
	return f.timeProvider.Now()
}

// initializeData populates all mock data with dynamic dates.
func (f *AirlineFixture) initializeData() {
	now := f.timeProvider.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// Tomorrow's flights
	tomorrow := today.AddDate(0, 0, 1)
	// Day after tomorrow's flights
	dayAfterTomorrow := today.AddDate(0, 0, 2)

	// Booking dates (in the past)
	fiveDaysAgo := today.AddDate(0, 0, -5)
	threeDaysAgo := today.AddDate(0, 0, -3)
	sevenDaysAgo := today.AddDate(0, 0, -7)

	// Initialize customers
	f.customers = map[string]*Customer{
		"C001": {
			ID:            "C001",
			Name:          "John Smith",
			Email:         "john.smith@email.com",
			Phone:         "+1-555-0123",
			FrequentFlyer: "gold",
			BookingIDs:    []string{"BK001", "BK002"},
		},
		"C002": {
			ID:            "C002",
			Name:          "Sarah Johnson",
			Email:         "sarah.j@email.com",
			Phone:         "+1-555-0456",
			FrequentFlyer: "platinum",
			BookingIDs:    []string{"BK003"},
		},
	}

	// Initialize flights with dynamic dates
	// Tomorrow's flights: AA100 (morning), AA101 (afternoon), AA102 (evening)
	f.flights = map[string]*Flight{
		"AA100": {
			FlightNumber:    "AA100",
			Origin:          "JFK",
			Destination:     "LAX",
			DepartureTime:   tomorrow.Add(8 * time.Hour),  // 8:00 AM
			ArrivalTime:     tomorrow.Add(11*time.Hour + 30*time.Minute),
			Aircraft:        "Boeing 777",
			Status:          "scheduled",
			AvailableSeats:  45,
			EconomyPrice:    299.00,
			BusinessPrice:   899.00,
			FirstClassPrice: 1599.00,
		},
		"AA101": {
			FlightNumber:    "AA101",
			Origin:          "JFK",
			Destination:     "LAX",
			DepartureTime:   tomorrow.Add(14 * time.Hour), // 2:00 PM
			ArrivalTime:     tomorrow.Add(17*time.Hour + 30*time.Minute),
			Aircraft:        "Airbus A320",
			Status:          "scheduled",
			AvailableSeats:  23,
			EconomyPrice:    329.00,
			BusinessPrice:   949.00,
			FirstClassPrice: 1699.00,
		},
		"AA102": {
			FlightNumber:    "AA102",
			Origin:          "JFK",
			Destination:     "LAX",
			DepartureTime:   tomorrow.Add(20 * time.Hour), // 8:00 PM
			ArrivalTime:     tomorrow.Add(23*time.Hour + 30*time.Minute),
			Aircraft:        "Boeing 787",
			Status:          "scheduled",
			AvailableSeats:  67,
			EconomyPrice:    279.00,
			BusinessPrice:   849.00,
			FirstClassPrice: 1499.00,
		},
		"AA200": {
			FlightNumber:    "AA200",
			Origin:          "JFK",
			Destination:     "LAX",
			DepartureTime:   dayAfterTomorrow.Add(9 * time.Hour), // 9:00 AM
			ArrivalTime:     dayAfterTomorrow.Add(12*time.Hour + 30*time.Minute),
			Aircraft:        "Boeing 777",
			Status:          "scheduled",
			AvailableSeats:  89,
			EconomyPrice:    269.00,
			BusinessPrice:   799.00,
			FirstClassPrice: 1399.00,
		},
	}

	// Initialize bookings with dynamic dates
	f.bookings = map[string]*Booking{
		"BK001": {
			BookingID:      "BK001",
			CustomerID:     "C001",
			FlightNumber:   "AA100",
			SeatNumber:     "12A",
			Class:          "economy",
			Status:         "confirmed",
			BookingDate:    fiveDaysAgo,
			TotalPrice:     299.00,
			MealPreference: "vegetarian",
		},
		"BK002": {
			BookingID:      "BK002",
			CustomerID:     "C001",
			FlightNumber:   "AA200",
			SeatNumber:     "3C",
			Class:          "business",
			Status:         "confirmed",
			BookingDate:    threeDaysAgo,
			TotalPrice:     799.00,
			MealPreference: "regular",
		},
		"BK003": {
			BookingID:      "BK003",
			CustomerID:     "C002",
			FlightNumber:   "AA101",
			SeatNumber:     "1A",
			Class:          "first",
			Status:         "confirmed",
			BookingDate:    sevenDaysAgo,
			TotalPrice:     1699.00,
			MealPreference: "kosher",
		},
	}

	// Initialize seats (static, not date-dependent)
	f.seats = map[string][]Seat{
		"AA100": {
			{SeatNumber: "1A", Class: "first", IsAvailable: false, IsWindow: true, IsAisle: false,
				HasExtraLeg: true},
			{SeatNumber: "1B", Class: "first", IsAvailable: true, IsWindow: false, IsAisle: true,
				HasExtraLeg: true},
			{SeatNumber: "2A", Class: "first", IsAvailable: true, IsWindow: true, IsAisle: false,
				HasExtraLeg: true},
			{SeatNumber: "3A", Class: "business", IsAvailable: true, IsWindow: true, IsAisle: false,
				HasExtraLeg: true},
			{SeatNumber: "3C", Class: "business", IsAvailable: false, IsWindow: false, IsAisle: true,
				HasExtraLeg: true},
			{SeatNumber: "10A", Class: "economy", IsAvailable: true, IsWindow: true, IsAisle: false,
				HasExtraLeg: false},
			{SeatNumber: "10B", Class: "economy", IsAvailable: true, IsWindow: false, IsAisle: false,
				HasExtraLeg: false},
			{SeatNumber: "10C", Class: "economy", IsAvailable: false, IsWindow: false, IsAisle: true,
				HasExtraLeg: false},
			{SeatNumber: "12A", Class: "economy", IsAvailable: false, IsWindow: true, IsAisle: false,
				HasExtraLeg: false},
			{SeatNumber: "15A", Class: "economy", IsAvailable: true, IsWindow: true, IsAisle: false,
				HasExtraLeg: true},
			{SeatNumber: "15C", Class: "economy", IsAvailable: true, IsWindow: false, IsAisle: true,
				HasExtraLeg: true},
		},
		"AA101": {
			{SeatNumber: "1A", Class: "first", IsAvailable: false, IsWindow: true, IsAisle: false,
				HasExtraLeg: true},
			{SeatNumber: "1B", Class: "first", IsAvailable: true, IsWindow: false, IsAisle: true,
				HasExtraLeg: true},
			{SeatNumber: "5A", Class: "business", IsAvailable: true, IsWindow: true, IsAisle: false,
				HasExtraLeg: true},
			{SeatNumber: "5C", Class: "business", IsAvailable: true, IsWindow: false, IsAisle: true,
				HasExtraLeg: true},
			{SeatNumber: "20A", Class: "economy", IsAvailable: true, IsWindow: true, IsAisle: false,
				HasExtraLeg: false},
		},
		"AA102": {
			// First class
			{SeatNumber: "1A", Class: "first", IsAvailable: true, IsWindow: true,
				IsAisle: false, HasExtraLeg: true},
			{SeatNumber: "1C", Class: "first", IsAvailable: false, IsWindow: false,
				IsAisle: true, HasExtraLeg: true},
			// Business
			{SeatNumber: "4A", Class: "business", IsAvailable: true, IsWindow: true,
				IsAisle: false, HasExtraLeg: true},
			{SeatNumber: "4C", Class: "business", IsAvailable: true, IsWindow: false,
				IsAisle: true, HasExtraLeg: true},
			{SeatNumber: "5A", Class: "business", IsAvailable: false, IsWindow: true,
				IsAisle: false, HasExtraLeg: true},
			// Economy exit row
			{SeatNumber: "14A", Class: "economy", IsAvailable: false, IsWindow: true,
				IsAisle: false, HasExtraLeg: true},
			{SeatNumber: "14B", Class: "economy", IsAvailable: true, IsWindow: false,
				IsAisle: false, HasExtraLeg: true},
			{SeatNumber: "14C", Class: "economy", IsAvailable: true, IsWindow: false,
				IsAisle: true, HasExtraLeg: true},
			{SeatNumber: "15A", Class: "economy", IsAvailable: true, IsWindow: true,
				IsAisle: false, HasExtraLeg: true},
			// Economy regular
			{SeatNumber: "22A", Class: "economy", IsAvailable: true, IsWindow: true,
				IsAisle: false, HasExtraLeg: false},
			{SeatNumber: "22B", Class: "economy", IsAvailable: true, IsWindow: false,
				IsAisle: false, HasExtraLeg: false},
			{SeatNumber: "22C", Class: "economy", IsAvailable: true, IsWindow: false,
				IsAisle: true, HasExtraLeg: false},
			{SeatNumber: "30A", Class: "economy", IsAvailable: false, IsWindow: true,
				IsAisle: false, HasExtraLeg: false},
			{SeatNumber: "30C", Class: "economy", IsAvailable: true, IsWindow: false,
				IsAisle: true, HasExtraLeg: false},
		},
	}

	// Initialize policies (static, not date-dependent)
	f.policies = []Policy{
		{
			Title: "Flight Change and Rescheduling Policy",
			Content: `Customers may change or reschedule their flights subject to the following:
- Changes made 24+ hours before departure: $50 change fee for economy, free for business/first class
- Changes made within 24 hours: $150 change fee for economy, $75 for business, free for first class
- Gold and Platinum frequent flyers receive one free change per booking
- Fare difference applies if new flight is more expensive
- If new flight is cheaper, difference is provided as travel credit`,
		},
		{
			Title: "Cancellation and Refund Policy",
			Content: `Cancellation terms vary by ticket type:
- Refundable tickets: Full refund minus $25 processing fee
- Non-refundable tickets: Travel credit minus $100 fee
- Within 24 hours of booking: Full refund regardless of ticket type
- Cancellations due to airline: Full refund plus compensation`,
		},
		{
			Title: "Baggage Policy",
			Content: `Baggage allowance by class:
- Economy: 1 carry-on (22x14x9 in), 1 checked bag (50 lbs) - $35 for 2nd bag
- Business: 2 carry-ons, 2 checked bags (70 lbs each) included
- First Class: 2 carry-ons, 3 checked bags (70 lbs each) included
- Overweight bags: $75 per bag over limit`,
		},
		{
			Title: "Frequent Flyer Benefits",
			Content: `Tier benefits:
- Bronze: Priority boarding, 10% bonus miles
- Silver: Priority boarding, lounge access on international, 25% bonus miles
- Gold: Free seat selection, 1 free change/cancellation, lounge access, 50% bonus miles
- Platinum: All Gold benefits plus free upgrades when available, 100% bonus miles`,
		},
	}
}

// -----------------------------------------------------------------------------
// Tool Methods
// -----------------------------------------------------------------------------

// GetCustomerInfoTool returns a tool that retrieves customer information.
func (f *AirlineFixture) GetCustomerInfoTool() *gent.ToolFunc[GetCustomerInfoInput, *Customer] {
	return gent.NewToolFunc(
		"get_customer_info",
		"Retrieve customer information by customer ID or email address",
		schema.Object(map[string]*schema.Property{
			"customer_id": schema.String("The customer's unique ID (e.g., C001)"),
			"email":       schema.String("The customer's email address"),
		}),
		func(ctx context.Context, input GetCustomerInfoInput) (*Customer, error) {
			if input.CustomerID != "" {
				if customer, exists := f.customers[input.CustomerID]; exists {
					return customer, nil
				}
				return nil, fmt.Errorf("customer not found with ID: %s", input.CustomerID)
			}
			if input.Email != "" {
				for _, customer := range f.customers {
					if customer.Email == input.Email {
						return customer, nil
					}
				}
				return nil, fmt.Errorf("customer not found with email: %s", input.Email)
			}
			return nil, fmt.Errorf("must provide either customer_id or email")
		},
	)
}

// GetBookingInfoTool returns a tool that retrieves booking details.
func (f *AirlineFixture) GetBookingInfoTool() *gent.ToolFunc[GetBookingInfoInput, *Booking] {
	return gent.NewToolFunc(
		"get_booking_info",
		"Retrieve booking details by booking ID",
		schema.Object(map[string]*schema.Property{
			"booking_id": schema.String("The booking reference ID (e.g., BK001)"),
		}, "booking_id"),
		func(ctx context.Context, input GetBookingInfoInput) (*Booking, error) {
			if input.BookingID == "" {
				return nil, fmt.Errorf("booking_id is required")
			}
			if booking, exists := f.bookings[input.BookingID]; exists {
				return booking, nil
			}
			return nil, fmt.Errorf("booking not found: %s", input.BookingID)
		},
	)
}

// GetFlightInfoTool returns a tool that retrieves flight information.
func (f *AirlineFixture) GetFlightInfoTool() *gent.ToolFunc[GetFlightInfoInput, *Flight] {
	return gent.NewToolFunc(
		"get_flight_info",
		"Retrieve flight information by flight number",
		schema.Object(map[string]*schema.Property{
			"flight_number": schema.String("The flight number (e.g., AA100)"),
		}, "flight_number"),
		func(ctx context.Context, input GetFlightInfoInput) (*Flight, error) {
			if input.FlightNumber == "" {
				return nil, fmt.Errorf("flight_number is required")
			}
			if flight, exists := f.flights[input.FlightNumber]; exists {
				return flight, nil
			}
			return nil, fmt.Errorf("flight not found: %s", input.FlightNumber)
		},
	)
}

// GetFlightSeatsInfoTool returns a tool that retrieves seat availability for a flight.
func (f *AirlineFixture) GetFlightSeatsInfoTool() *gent.ToolFunc[GetFlightSeatsInfoInput, []Seat] {
	return gent.NewToolFunc(
		"get_flight_seats_info",
		"Retrieve seat availability and details for a specific flight",
		schema.Object(map[string]*schema.Property{
			"flight_number":  schema.String("The flight number (e.g., AA100)"),
			"class":          schema.String("Filter by class: economy, business, or first (optional)"),
			"available_only": schema.Boolean("If true, only return available seats"),
		}, "flight_number"),
		func(ctx context.Context, input GetFlightSeatsInfoInput) ([]Seat, error) {
			if input.FlightNumber == "" {
				return nil, fmt.Errorf("flight_number is required")
			}
			seats, exists := f.seats[input.FlightNumber]
			if !exists {
				return nil, fmt.Errorf("no seat data for flight: %s", input.FlightNumber)
			}

			var filtered []Seat
			for _, seat := range seats {
				if input.Class != "" && seat.Class != input.Class {
					continue
				}
				if input.AvailableOnly && !seat.IsAvailable {
					continue
				}
				filtered = append(filtered, seat)
			}
			return filtered, nil
		},
	)
}

// SearchAirlinePolicyTool returns a tool that searches airline policies.
func (f *AirlineFixture) SearchAirlinePolicyTool() *gent.ToolFunc[SearchAirlinePolicyInput, []Policy] {
	return gent.NewToolFunc(
		"search_airline_policy",
		"Search airline policies by keyword (e.g., 'cancellation', 'baggage', 'change')",
		schema.Object(map[string]*schema.Property{
			"keyword": schema.String("Keyword to search for in policy titles and content"),
		}, "keyword"),
		func(ctx context.Context, input SearchAirlinePolicyInput) ([]Policy, error) {
			if input.Keyword == "" {
				return nil, fmt.Errorf("keyword is required")
			}

			var results []Policy
			for _, policy := range f.policies {
				if testutil.ContainsIgnoreCase(policy.Title, input.Keyword) ||
					testutil.ContainsIgnoreCase(policy.Content, input.Keyword) {
					results = append(results, policy)
				}
			}
			if len(results) == 0 {
				return nil, fmt.Errorf("no policies found matching: %s", input.Keyword)
			}
			return results, nil
		},
	)
}

// SearchFlightScheduleTool returns a tool that searches for available flights.
func (f *AirlineFixture) SearchFlightScheduleTool() *gent.ToolFunc[SearchFlightScheduleInput, []*Flight] {
	return gent.NewToolFunc(
		"search_flight_schedule",
		"Search for available flights between two airports on a specific date",
		schema.Object(map[string]*schema.Property{
			"origin":      schema.String("Origin airport code (e.g., JFK)"),
			"destination": schema.String("Destination airport code (e.g., LAX)"),
			"date":        schema.String("Travel date in YYYY-MM-DD format"),
		}, "origin", "destination", "date"),
		func(ctx context.Context, input SearchFlightScheduleInput) ([]*Flight, error) {
			if input.Origin == "" || input.Destination == "" || input.Date == "" {
				return nil, fmt.Errorf("origin, destination, and date are required")
			}

			date, err := time.Parse("2006-01-02", input.Date)
			if err != nil {
				return nil, fmt.Errorf("invalid date format, use YYYY-MM-DD: %v", err)
			}

			var results []*Flight
			for _, flight := range f.flights {
				if flight.Origin == input.Origin && flight.Destination == input.Destination {
					flightDate := flight.DepartureTime.Truncate(24 * time.Hour)
					searchDate := date.Truncate(24 * time.Hour)
					if flightDate.Equal(searchDate) && flight.AvailableSeats > 0 {
						results = append(results, flight)
					}
				}
			}
			if len(results) == 0 {
				return nil, fmt.Errorf("no flights found from %s to %s on %s",
					input.Origin, input.Destination, input.Date)
			}
			return results, nil
		},
	)
}

// RescheduleBookingTool returns a tool that reschedules a booking to a new flight.
func (f *AirlineFixture) RescheduleBookingTool() *gent.ToolFunc[RescheduleBookingInput,
	*RescheduleBookingResult] {
	return gent.NewToolFunc(
		"reschedule_booking",
		"Reschedule an existing booking to a different flight",
		schema.Object(map[string]*schema.Property{
			"booking_id":        schema.String("The booking ID to reschedule"),
			"new_flight_number": schema.String("The new flight number to reschedule to"),
			"new_seat_number":   schema.String("The desired seat on the new flight (optional)"),
		}, "booking_id", "new_flight_number"),
		func(ctx context.Context, input RescheduleBookingInput) (*RescheduleBookingResult, error) {
			booking, exists := f.bookings[input.BookingID]
			if !exists {
				return nil, fmt.Errorf("booking not found: %s", input.BookingID)
			}

			newFlight, exists := f.flights[input.NewFlightNumber]
			if !exists {
				return nil, fmt.Errorf("flight not found: %s", input.NewFlightNumber)
			}

			if newFlight.AvailableSeats <= 0 {
				return nil, fmt.Errorf("no available seats on flight %s", input.NewFlightNumber)
			}

			// Calculate fees based on class and frequent flyer status
			customer := f.customers[booking.CustomerID]
			changeFee := 50.0
			if booking.Class == "business" {
				changeFee = 0
			} else if booking.Class == "first" {
				changeFee = 0
			}
			if customer != nil && (customer.FrequentFlyer == "gold" ||
				customer.FrequentFlyer == "platinum") {
				changeFee = 0
			}

			// Calculate fare difference
			var oldPrice, newPrice float64
			oldFlight := f.flights[booking.FlightNumber]
			switch booking.Class {
			case "economy":
				oldPrice = oldFlight.EconomyPrice
				newPrice = newFlight.EconomyPrice
			case "business":
				oldPrice = oldFlight.BusinessPrice
				newPrice = newFlight.BusinessPrice
			case "first":
				oldPrice = oldFlight.FirstClassPrice
				newPrice = newFlight.FirstClassPrice
			}

			fareDiff := newPrice - oldPrice
			var fareCredit float64
			if fareDiff < 0 {
				fareCredit = -fareDiff
				fareDiff = 0
			}

			// Assign seat if not specified
			newSeat := input.NewSeatNumber
			if newSeat == "" {
				newSeat = "TBD (assigned at check-in)"
			}

			// Update mock data
			oldFlightNum := booking.FlightNumber
			booking.FlightNumber = input.NewFlightNumber
			booking.SeatNumber = newSeat
			booking.TotalPrice = newPrice

			msg := fmt.Sprintf(
				"Successfully rescheduled from %s to %s. "+
					"Total charge: $%.2f.",
				oldFlightNum, input.NewFlightNumber,
				changeFee+fareDiff,
			)
			if fareCredit > 0 {
				msg += fmt.Sprintf(
					" A fare credit of $%.2f will be "+
						"applied to your account within "+
						"3-5 business days.",
					fareCredit,
				)
			}

			return &RescheduleBookingResult{
				Success:        true,
				OldFlightNum:   oldFlightNum,
				NewFlightNum:   input.NewFlightNumber,
				NewSeatNumber:  newSeat,
				ChangeFee:      changeFee,
				FareDifference: fareDiff,
				FareCredit:     fareCredit,
				TotalCharge:    changeFee + fareDiff,
				Message:        msg,
			}, nil
		},
	)
}

// CancelBookingTool returns a tool that cancels a booking.
func (f *AirlineFixture) CancelBookingTool() *gent.ToolFunc[CancelBookingInput, *CancelBookingResult] {
	return gent.NewToolFunc(
		"cancel_booking",
		"Cancel an existing booking and process refund",
		schema.Object(map[string]*schema.Property{
			"booking_id": schema.String("The booking ID to cancel"),
			"reason":     schema.String("Reason for cancellation"),
		}, "booking_id"),
		func(ctx context.Context, input CancelBookingInput) (*CancelBookingResult, error) {
			booking, exists := f.bookings[input.BookingID]
			if !exists {
				return nil, fmt.Errorf("booking not found: %s", input.BookingID)
			}

			if booking.Status == "cancelled" {
				return nil, fmt.Errorf("booking %s is already cancelled", input.BookingID)
			}

			// Calculate refund (simplified)
			refundAmount := booking.TotalPrice - 100.0 // $100 cancellation fee
			if refundAmount < 0 {
				refundAmount = 0
			}

			booking.Status = "cancelled"

			return &CancelBookingResult{
				Success:      true,
				BookingID:    input.BookingID,
				RefundAmount: refundAmount,
				RefundType:   "credit",
				Message: fmt.Sprintf("Booking %s cancelled. Travel credit of $%.2f issued.",
					input.BookingID, refundAmount),
			}, nil
		},
	)
}

// SendNotificationTool returns a tool that sends notifications to customers.
func (f *AirlineFixture) SendNotificationTool() *gent.ToolFunc[SendNotificationInput,
	*SendNotificationResult] {
	return gent.NewToolFunc(
		"send_notification",
		"Send email or SMS notification to customer about booking changes",
		schema.Object(map[string]*schema.Property{
			"customer_id": schema.String("Customer ID to notify"),
			"method":      schema.String("Notification method: email or sms").Enum("email", "sms"),
			"subject":     schema.String("Subject of the notification"),
			"message":     schema.String("The notification message content"),
		}, "customer_id", "method", "message"),
		func(ctx context.Context, input SendNotificationInput) (*SendNotificationResult, error) {
			customer, exists := f.customers[input.CustomerID]
			if !exists {
				return nil, fmt.Errorf("customer not found: %s", input.CustomerID)
			}

			if input.Method != "email" && input.Method != "sms" {
				return nil, fmt.Errorf("invalid method: %s (must be email or sms)", input.Method)
			}

			destination := customer.Email
			if input.Method == "sms" {
				destination = customer.Phone
			}

			return &SendNotificationResult{
				Success: true,
				Method:  input.Method,
				Message: fmt.Sprintf("Notification sent via %s to %s: %s",
					input.Method, destination, input.Message),
			}, nil
		},
	)
}

// RegisterAllTools registers all airline tools from this fixture to a toolchain.
func (f *AirlineFixture) RegisterAllTools(tc gent.ToolChain) {
	tc.RegisterTool(f.GetCustomerInfoTool())
	tc.RegisterTool(f.GetBookingInfoTool())
	tc.RegisterTool(f.GetFlightInfoTool())
	tc.RegisterTool(f.GetFlightSeatsInfoTool())
	tc.RegisterTool(f.SearchAirlinePolicyTool())
	tc.RegisterTool(f.SearchFlightScheduleTool())
	tc.RegisterTool(f.RescheduleBookingTool())
	tc.RegisterTool(f.CancelBookingTool())
	tc.RegisterTool(f.SendNotificationTool())
}

// TomorrowDate returns tomorrow's date in YYYY-MM-DD format.
// Useful for tests that need to search for flights.
func (f *AirlineFixture) TomorrowDate() string {
	now := f.timeProvider.Now()
	tomorrow := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	return tomorrow.Format("2006-01-02")
}

// DayAfterTomorrowDate returns the day after tomorrow's date in YYYY-MM-DD format.
func (f *AirlineFixture) DayAfterTomorrowDate() string {
	now := f.timeProvider.Now()
	dat := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 2)
	return dat.Format("2006-01-02")
}

