package airline

import (
	"context"
	"fmt"
	"time"

	"github.com/rickchristie/gent"
	"github.com/rickchristie/gent/schema"
)

// -----------------------------------------------------------------------------
// Mock Data Types
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
// Mock Data Store
// -----------------------------------------------------------------------------

var mockCustomers = map[string]*Customer{
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

var mockFlights = map[string]*Flight{
	"AA100": {
		FlightNumber:    "AA100",
		Origin:          "JFK",
		Destination:     "LAX",
		DepartureTime:   time.Date(2025, 2, 15, 8, 0, 0, 0, time.UTC),
		ArrivalTime:     time.Date(2025, 2, 15, 11, 30, 0, 0, time.UTC),
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
		DepartureTime:   time.Date(2025, 2, 15, 14, 0, 0, 0, time.UTC),
		ArrivalTime:     time.Date(2025, 2, 15, 17, 30, 0, 0, time.UTC),
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
		DepartureTime:   time.Date(2025, 2, 15, 20, 0, 0, 0, time.UTC),
		ArrivalTime:     time.Date(2025, 2, 15, 23, 30, 0, 0, time.UTC),
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
		DepartureTime:   time.Date(2025, 2, 16, 9, 0, 0, 0, time.UTC),
		ArrivalTime:     time.Date(2025, 2, 16, 12, 30, 0, 0, time.UTC),
		Aircraft:        "Boeing 777",
		Status:          "scheduled",
		AvailableSeats:  89,
		EconomyPrice:    269.00,
		BusinessPrice:   799.00,
		FirstClassPrice: 1399.00,
	},
}

var mockBookings = map[string]*Booking{
	"BK001": {
		BookingID:      "BK001",
		CustomerID:     "C001",
		FlightNumber:   "AA100",
		SeatNumber:     "12A",
		Class:          "economy",
		Status:         "confirmed",
		BookingDate:    time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC),
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
		BookingDate:    time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
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
		BookingDate:    time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC),
		TotalPrice:     1699.00,
		MealPreference: "kosher",
	},
}

var mockSeats = map[string][]Seat{
	"AA100": {
		{SeatNumber: "1A", Class: "first", IsAvailable: false, IsWindow: true, IsAisle: false, HasExtraLeg: true},
		{SeatNumber: "1B", Class: "first", IsAvailable: true, IsWindow: false, IsAisle: true, HasExtraLeg: true},
		{SeatNumber: "2A", Class: "first", IsAvailable: true, IsWindow: true, IsAisle: false, HasExtraLeg: true},
		{SeatNumber: "3A", Class: "business", IsAvailable: true, IsWindow: true, IsAisle: false, HasExtraLeg: true},
		{SeatNumber: "3C", Class: "business", IsAvailable: false, IsWindow: false, IsAisle: true, HasExtraLeg: true},
		{SeatNumber: "10A", Class: "economy", IsAvailable: true, IsWindow: true, IsAisle: false, HasExtraLeg: false},
		{SeatNumber: "10B", Class: "economy", IsAvailable: true, IsWindow: false, IsAisle: false, HasExtraLeg: false},
		{SeatNumber: "10C", Class: "economy", IsAvailable: false, IsWindow: false, IsAisle: true, HasExtraLeg: false},
		{SeatNumber: "12A", Class: "economy", IsAvailable: false, IsWindow: true, IsAisle: false, HasExtraLeg: false},
		{SeatNumber: "15A", Class: "economy", IsAvailable: true, IsWindow: true, IsAisle: false, HasExtraLeg: true},
		{SeatNumber: "15C", Class: "economy", IsAvailable: true, IsWindow: false, IsAisle: true, HasExtraLeg: true},
	},
	"AA101": {
		{SeatNumber: "1A", Class: "first", IsAvailable: false, IsWindow: true, IsAisle: false, HasExtraLeg: true},
		{SeatNumber: "1B", Class: "first", IsAvailable: true, IsWindow: false, IsAisle: true, HasExtraLeg: true},
		{SeatNumber: "5A", Class: "business", IsAvailable: true, IsWindow: true, IsAisle: false, HasExtraLeg: true},
		{SeatNumber: "5C", Class: "business", IsAvailable: true, IsWindow: false, IsAisle: true, HasExtraLeg: true},
		{SeatNumber: "20A", Class: "economy", IsAvailable: true, IsWindow: true, IsAisle: false, HasExtraLeg: false},
	},
}

var mockPolicies = []Policy{
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

// -----------------------------------------------------------------------------
// Tool Implementations
// -----------------------------------------------------------------------------

// GetCustomerInfoTool returns a tool that retrieves customer information by ID or email.
func GetCustomerInfoTool() *gent.ToolFunc[GetCustomerInfoInput, *Customer] {
	return gent.NewToolFunc(
		"get_customer_info",
		"Retrieve customer information by customer ID or email address",
		schema.Object(map[string]*schema.Property{
			"customer_id": schema.String("The customer's unique ID (e.g., C001)"),
			"email":       schema.String("The customer's email address"),
		}),
		func(ctx context.Context, input GetCustomerInfoInput) (*Customer, error) {
			if input.CustomerID != "" {
				if customer, exists := mockCustomers[input.CustomerID]; exists {
					return customer, nil
				}
				return nil, fmt.Errorf("customer not found with ID: %s", input.CustomerID)
			}
			if input.Email != "" {
				for _, customer := range mockCustomers {
					if customer.Email == input.Email {
						return customer, nil
					}
				}
				return nil, fmt.Errorf("customer not found with email: %s", input.Email)
			}
			return nil, fmt.Errorf("must provide either customer_id or email")
		},
		nil,
	)
}

// GetBookingInfoTool returns a tool that retrieves booking details.
func GetBookingInfoTool() *gent.ToolFunc[GetBookingInfoInput, *Booking] {
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
			if booking, exists := mockBookings[input.BookingID]; exists {
				return booking, nil
			}
			return nil, fmt.Errorf("booking not found: %s", input.BookingID)
		},
		nil,
	)
}

// GetFlightInfoTool returns a tool that retrieves flight information.
func GetFlightInfoTool() *gent.ToolFunc[GetFlightInfoInput, *Flight] {
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
			if flight, exists := mockFlights[input.FlightNumber]; exists {
				return flight, nil
			}
			return nil, fmt.Errorf("flight not found: %s", input.FlightNumber)
		},
		nil,
	)
}

// GetFlightSeatsInfoTool returns a tool that retrieves seat availability for a flight.
func GetFlightSeatsInfoTool() *gent.ToolFunc[GetFlightSeatsInfoInput, []Seat] {
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
			seats, exists := mockSeats[input.FlightNumber]
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
		nil,
	)
}

// SearchAirlinePolicyTool returns a tool that searches airline policies.
func SearchAirlinePolicyTool() *gent.ToolFunc[SearchAirlinePolicyInput, []Policy] {
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
			for _, policy := range mockPolicies {
				if containsIgnoreCase(policy.Title, input.Keyword) ||
					containsIgnoreCase(policy.Content, input.Keyword) {
					results = append(results, policy)
				}
			}
			if len(results) == 0 {
				return nil, fmt.Errorf("no policies found matching: %s", input.Keyword)
			}
			return results, nil
		},
		nil,
	)
}

// SearchFlightScheduleTool returns a tool that searches for available flights.
func SearchFlightScheduleTool() *gent.ToolFunc[SearchFlightScheduleInput, []*Flight] {
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
			for _, flight := range mockFlights {
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
		nil,
	)
}

// RescheduleBookingResult is the result of rescheduling a booking.
type RescheduleBookingResult struct {
	Success        bool    `json:"success"`
	OldFlightNum   string  `json:"old_flight_number"`
	NewFlightNum   string  `json:"new_flight_number"`
	NewSeatNumber  string  `json:"new_seat_number"`
	ChangeFee      float64 `json:"change_fee"`
	FareDifference float64 `json:"fare_difference"`
	TotalCharge    float64 `json:"total_charge"`
	Message        string  `json:"message"`
}

// RescheduleBookingTool returns a tool that reschedules a booking to a new flight.
func RescheduleBookingTool() *gent.ToolFunc[RescheduleBookingInput, *RescheduleBookingResult] {
	return gent.NewToolFunc(
		"reschedule_booking",
		"Reschedule an existing booking to a different flight",
		schema.Object(map[string]*schema.Property{
			"booking_id":        schema.String("The booking ID to reschedule"),
			"new_flight_number": schema.String("The new flight number to reschedule to"),
			"new_seat_number":   schema.String("The desired seat on the new flight (optional)"),
		}, "booking_id", "new_flight_number"),
		func(ctx context.Context, input RescheduleBookingInput) (*RescheduleBookingResult, error) {
			booking, exists := mockBookings[input.BookingID]
			if !exists {
				return nil, fmt.Errorf("booking not found: %s", input.BookingID)
			}

			newFlight, exists := mockFlights[input.NewFlightNumber]
			if !exists {
				return nil, fmt.Errorf("flight not found: %s", input.NewFlightNumber)
			}

			if newFlight.AvailableSeats <= 0 {
				return nil, fmt.Errorf("no available seats on flight %s", input.NewFlightNumber)
			}

			// Calculate fees based on class and frequent flyer status
			customer := mockCustomers[booking.CustomerID]
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
			oldFlight := mockFlights[booking.FlightNumber]
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
			if fareDiff < 0 {
				fareDiff = 0 // Credit would be issued separately
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

			return &RescheduleBookingResult{
				Success:        true,
				OldFlightNum:   oldFlightNum,
				NewFlightNum:   input.NewFlightNumber,
				NewSeatNumber:  newSeat,
				ChangeFee:      changeFee,
				FareDifference: fareDiff,
				TotalCharge:    changeFee + fareDiff,
				Message: fmt.Sprintf("Successfully rescheduled from %s to %s. "+
					"Total charge: $%.2f", oldFlightNum, input.NewFlightNumber, changeFee+fareDiff),
			}, nil
		},
		nil,
	)
}

// CancelBookingResult is the result of cancelling a booking.
type CancelBookingResult struct {
	Success      bool    `json:"success"`
	BookingID    string  `json:"booking_id"`
	RefundAmount float64 `json:"refund_amount"`
	RefundType   string  `json:"refund_type"` // cash, credit
	Message      string  `json:"message"`
}

// CancelBookingTool returns a tool that cancels a booking.
func CancelBookingTool() *gent.ToolFunc[CancelBookingInput, *CancelBookingResult] {
	return gent.NewToolFunc(
		"cancel_booking",
		"Cancel an existing booking and process refund",
		schema.Object(map[string]*schema.Property{
			"booking_id": schema.String("The booking ID to cancel"),
			"reason":     schema.String("Reason for cancellation"),
		}, "booking_id"),
		func(ctx context.Context, input CancelBookingInput) (*CancelBookingResult, error) {
			booking, exists := mockBookings[input.BookingID]
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
		nil,
	)
}

// SendNotificationResult is the result of sending a notification.
type SendNotificationResult struct {
	Success bool   `json:"success"`
	Method  string `json:"method"`
	Message string `json:"message"`
}

// SendNotificationTool returns a tool that sends notifications to customers.
func SendNotificationTool() *gent.ToolFunc[SendNotificationInput, *SendNotificationResult] {
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
			customer, exists := mockCustomers[input.CustomerID]
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
		nil,
	)
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

func containsIgnoreCase(s, substr string) bool {
	sLower := make([]byte, len(s))
	substrLower := make([]byte, len(substr))
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			sLower[i] = s[i] + 32
		} else {
			sLower[i] = s[i]
		}
	}
	for i := 0; i < len(substr); i++ {
		if substr[i] >= 'A' && substr[i] <= 'Z' {
			substrLower[i] = substr[i] + 32
		} else {
			substrLower[i] = substr[i]
		}
	}

	for i := 0; i <= len(sLower)-len(substrLower); i++ {
		match := true
		for j := 0; j < len(substrLower); j++ {
			if sLower[i+j] != substrLower[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// RegisterAllTools registers all airline tools to a toolchain.
func RegisterAllTools(tc gent.ToolChain) {
	tc.RegisterTool(GetCustomerInfoTool())
	tc.RegisterTool(GetBookingInfoTool())
	tc.RegisterTool(GetFlightInfoTool())
	tc.RegisterTool(GetFlightSeatsInfoTool())
	tc.RegisterTool(SearchAirlinePolicyTool())
	tc.RegisterTool(SearchFlightScheduleTool())
	tc.RegisterTool(RescheduleBookingTool())
	tc.RegisterTool(CancelBookingTool())
	tc.RegisterTool(SendNotificationTool())
}
