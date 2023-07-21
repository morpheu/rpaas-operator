/*
Reverse Proxy as a Service

The presented API definition (formally called as RPaaS v2 API) is a superset of [Tsuru Service API] and the [legacy RPaaS][RPaaS v1 API] (aka RPaaS v1).  Source code: [github.com/tsuru/rpaas-operator](https://github.com/tsuru/rpaas-operator.git)  [Tsuru Service API]: https://app.swaggerhub.com/apis/tsuru/tsuru-service_api [RPaaS v1 API]: https://raw.githubusercontent.com/tsuru/rpaas/master/rpaas/api.py

API version: v2
Contact: tsuru@g.globo
*/

// Code generated by OpenAPI Generator (https://openapi-generator.tech); DO NOT EDIT.

package autogenerated

import (
	"encoding/json"
)

// checks if the CreateInstance type satisfies the MappedNullable interface at compile time
var _ MappedNullable = &CreateInstance{}

// CreateInstance struct for CreateInstance
type CreateInstance struct {
	Name        string                    `json:"name"`
	Plan        string                    `json:"plan"`
	Team        string                    `json:"team"`
	Description *string                   `json:"description,omitempty"`
	Tags        []string                  `json:"tags,omitempty"`
	Parameters  *CreateInstanceParameters `json:"parameters,omitempty"`
}

// NewCreateInstance instantiates a new CreateInstance object
// This constructor will assign default values to properties that have it defined,
// and makes sure properties required by API are set, but the set of arguments
// will change when the set of required properties is changed
func NewCreateInstance(name string, plan string, team string) *CreateInstance {
	this := CreateInstance{}
	this.Name = name
	this.Plan = plan
	this.Team = team
	return &this
}

// NewCreateInstanceWithDefaults instantiates a new CreateInstance object
// This constructor will only assign default values to properties that have it defined,
// but it doesn't guarantee that properties required by API are set
func NewCreateInstanceWithDefaults() *CreateInstance {
	this := CreateInstance{}
	return &this
}

// GetName returns the Name field value
func (o *CreateInstance) GetName() string {
	if o == nil {
		var ret string
		return ret
	}

	return o.Name
}

// GetNameOk returns a tuple with the Name field value
// and a boolean to check if the value has been set.
func (o *CreateInstance) GetNameOk() (*string, bool) {
	if o == nil {
		return nil, false
	}
	return &o.Name, true
}

// SetName sets field value
func (o *CreateInstance) SetName(v string) {
	o.Name = v
}

// GetPlan returns the Plan field value
func (o *CreateInstance) GetPlan() string {
	if o == nil {
		var ret string
		return ret
	}

	return o.Plan
}

// GetPlanOk returns a tuple with the Plan field value
// and a boolean to check if the value has been set.
func (o *CreateInstance) GetPlanOk() (*string, bool) {
	if o == nil {
		return nil, false
	}
	return &o.Plan, true
}

// SetPlan sets field value
func (o *CreateInstance) SetPlan(v string) {
	o.Plan = v
}

// GetTeam returns the Team field value
func (o *CreateInstance) GetTeam() string {
	if o == nil {
		var ret string
		return ret
	}

	return o.Team
}

// GetTeamOk returns a tuple with the Team field value
// and a boolean to check if the value has been set.
func (o *CreateInstance) GetTeamOk() (*string, bool) {
	if o == nil {
		return nil, false
	}
	return &o.Team, true
}

// SetTeam sets field value
func (o *CreateInstance) SetTeam(v string) {
	o.Team = v
}

// GetDescription returns the Description field value if set, zero value otherwise.
func (o *CreateInstance) GetDescription() string {
	if o == nil || IsNil(o.Description) {
		var ret string
		return ret
	}
	return *o.Description
}

// GetDescriptionOk returns a tuple with the Description field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *CreateInstance) GetDescriptionOk() (*string, bool) {
	if o == nil || IsNil(o.Description) {
		return nil, false
	}
	return o.Description, true
}

// HasDescription returns a boolean if a field has been set.
func (o *CreateInstance) HasDescription() bool {
	if o != nil && !IsNil(o.Description) {
		return true
	}

	return false
}

// SetDescription gets a reference to the given string and assigns it to the Description field.
func (o *CreateInstance) SetDescription(v string) {
	o.Description = &v
}

// GetTags returns the Tags field value if set, zero value otherwise.
func (o *CreateInstance) GetTags() []string {
	if o == nil || IsNil(o.Tags) {
		var ret []string
		return ret
	}
	return o.Tags
}

// GetTagsOk returns a tuple with the Tags field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *CreateInstance) GetTagsOk() ([]string, bool) {
	if o == nil || IsNil(o.Tags) {
		return nil, false
	}
	return o.Tags, true
}

// HasTags returns a boolean if a field has been set.
func (o *CreateInstance) HasTags() bool {
	if o != nil && !IsNil(o.Tags) {
		return true
	}

	return false
}

// SetTags gets a reference to the given []string and assigns it to the Tags field.
func (o *CreateInstance) SetTags(v []string) {
	o.Tags = v
}

// GetParameters returns the Parameters field value if set, zero value otherwise.
func (o *CreateInstance) GetParameters() CreateInstanceParameters {
	if o == nil || IsNil(o.Parameters) {
		var ret CreateInstanceParameters
		return ret
	}
	return *o.Parameters
}

// GetParametersOk returns a tuple with the Parameters field value if set, nil otherwise
// and a boolean to check if the value has been set.
func (o *CreateInstance) GetParametersOk() (*CreateInstanceParameters, bool) {
	if o == nil || IsNil(o.Parameters) {
		return nil, false
	}
	return o.Parameters, true
}

// HasParameters returns a boolean if a field has been set.
func (o *CreateInstance) HasParameters() bool {
	if o != nil && !IsNil(o.Parameters) {
		return true
	}

	return false
}

// SetParameters gets a reference to the given CreateInstanceParameters and assigns it to the Parameters field.
func (o *CreateInstance) SetParameters(v CreateInstanceParameters) {
	o.Parameters = &v
}

func (o CreateInstance) MarshalJSON() ([]byte, error) {
	toSerialize, err := o.ToMap()
	if err != nil {
		return []byte{}, err
	}
	return json.Marshal(toSerialize)
}

func (o CreateInstance) ToMap() (map[string]interface{}, error) {
	toSerialize := map[string]interface{}{}
	toSerialize["name"] = o.Name
	toSerialize["plan"] = o.Plan
	toSerialize["team"] = o.Team
	if !IsNil(o.Description) {
		toSerialize["description"] = o.Description
	}
	if !IsNil(o.Tags) {
		toSerialize["tags"] = o.Tags
	}
	if !IsNil(o.Parameters) {
		toSerialize["parameters"] = o.Parameters
	}
	return toSerialize, nil
}

type NullableCreateInstance struct {
	value *CreateInstance
	isSet bool
}

func (v NullableCreateInstance) Get() *CreateInstance {
	return v.value
}

func (v *NullableCreateInstance) Set(val *CreateInstance) {
	v.value = val
	v.isSet = true
}

func (v NullableCreateInstance) IsSet() bool {
	return v.isSet
}

func (v *NullableCreateInstance) Unset() {
	v.value = nil
	v.isSet = false
}

func NewNullableCreateInstance(val *CreateInstance) *NullableCreateInstance {
	return &NullableCreateInstance{value: val, isSet: true}
}

func (v NullableCreateInstance) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.value)
}

func (v *NullableCreateInstance) UnmarshalJSON(src []byte) error {
	v.isSet = true
	return json.Unmarshal(src, &v.value)
}
