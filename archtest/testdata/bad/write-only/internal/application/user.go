package application

import accounttype "example.com/app/accounttype"

type User struct {
	AccountTypeID int
	AccountRole   struct{ VehicleTypes []int }
}

func f(u User, row User) {
	if u.AccountTypeID <= 3 {
	}
	switch u.AccountTypeID {
	}
	g(row.AccountTypeID)
	_ = u.AccountRole.VehicleTypes
	u.AccountTypeID = 5
	u.AccountRole.VehicleTypes = nil
	v := User{AccountTypeID: 5}
	_ = v
	_ = accounttype.AccountTypeID
}

func g(int) {}
